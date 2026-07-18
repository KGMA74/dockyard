package v2

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"dockyard/internal/events"
	"dockyard/internal/registry"
	"dockyard/internal/storage"
	"dockyard/internal/storage/storagetest"
)

// fakeUpstream is a minimal V2 registry serving one manifest + one blob, and
// counting requests so tests can assert cache behavior.
type fakeUpstream struct {
	manifest  atomic.Pointer[[]byte]
	blob      []byte
	blobDgst  string
	requests  atomic.Int64
	unreached atomic.Bool
}

func (f *fakeUpstream) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.unreached.Load() {
			http.Error(w, "upstream down", http.StatusServiceUnavailable)
			return
		}
		f.requests.Add(1)
		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
		case "/v2/lib/app/manifests/v1":
			m := *f.manifest.Load()
			w.Header().Set("Docker-Content-Digest", storagetest.Digest(m))
			w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
			_, _ = w.Write(m)
		case "/v2/lib/app/blobs/" + f.blobDgst:
			_, _ = w.Write(f.blob)
		default:
			http.NotFound(w, r)
		}
	})
}

func newMirrorFixture(t *testing.T, ttl time.Duration) (*httptest.Server, *fakeUpstream, *Mirror) {
	t.Helper()
	blob := []byte("layer-content-for-mirror")
	up := &fakeUpstream{blob: blob, blobDgst: storagetest.Digest(blob)}
	manifest := storagetest.ManifestFor(storagetest.Digest([]byte("cfg")), up.blobDgst)
	up.manifest.Store(&manifest)

	upstream := httptest.NewServer(up.handler())
	t.Cleanup(upstream.Close)

	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := registry.NewClient(upstream.URL, "", "")
	m := NewMirror(backend, events.NewHub(), client, ttl)
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	return srv, up, m
}

func get(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func TestMirrorMissFillHit(t *testing.T) {
	srv, up, m := newMirrorFixture(t, time.Hour)
	manifest := *up.manifest.Load()

	// Miss: fetched from upstream, cached, served.
	status, body := get(t, srv.URL+"/v2/lib/app/manifests/v1")
	if status != http.StatusOK || !bytes.Equal(body, manifest) {
		t.Fatalf("first pull = %d, content ok=%v", status, bytes.Equal(body, manifest))
	}
	status, body = get(t, srv.URL+"/v2/lib/app/blobs/"+up.blobDgst)
	if status != http.StatusOK || !bytes.Equal(body, up.blob) {
		t.Fatalf("blob pull = %d", status)
	}
	after := up.requests.Load()

	// Hit: served locally, upstream untouched (tag still fresh).
	status, _ = get(t, srv.URL+"/v2/lib/app/manifests/v1")
	if status != http.StatusOK {
		t.Fatal("second manifest pull failed")
	}
	status, _ = get(t, srv.URL+"/v2/lib/app/blobs/"+up.blobDgst)
	if status != http.StatusOK {
		t.Fatal("second blob pull failed")
	}
	if up.requests.Load() != after {
		t.Errorf("upstream hit again on cached content: %d → %d requests", after, up.requests.Load())
	}
	hits, misses := m.Stats()
	if hits == 0 || misses == 0 {
		t.Errorf("stats = %d hits / %d misses, want both > 0", hits, misses)
	}
}

func TestMirrorTagTTLRevalidation(t *testing.T) {
	srv, up, _ := newMirrorFixture(t, time.Millisecond)

	if status, _ := get(t, srv.URL+"/v2/lib/app/manifests/v1"); status != http.StatusOK {
		t.Fatal("first pull failed")
	}

	// Upstream retags v1 to a new manifest; after TTL the mirror must pick it up.
	newManifest := storagetest.ManifestFor(storagetest.Digest([]byte("cfg-v2")))
	up.manifest.Store(&newManifest)
	time.Sleep(5 * time.Millisecond)

	status, body := get(t, srv.URL+"/v2/lib/app/manifests/v1")
	if status != http.StatusOK || !bytes.Equal(body, newManifest) {
		t.Errorf("after TTL: status=%d, got old manifest=%v", status, !bytes.Equal(body, newManifest))
	}
}

func TestMirrorServesStaleWhenUpstreamDown(t *testing.T) {
	srv, up, _ := newMirrorFixture(t, time.Millisecond)
	manifest := *up.manifest.Load()

	if status, _ := get(t, srv.URL+"/v2/lib/app/manifests/v1"); status != http.StatusOK {
		t.Fatal("first pull failed")
	}
	up.unreached.Store(true)
	time.Sleep(5 * time.Millisecond) // TTL expired → revalidation will fail

	status, body := get(t, srv.URL+"/v2/lib/app/manifests/v1")
	if status != http.StatusOK || !bytes.Equal(body, manifest) {
		t.Errorf("stale serve = %d, content ok=%v", status, bytes.Equal(body, manifest))
	}

	// But an uncached image cannot be invented.
	if status, _ := get(t, srv.URL+"/v2/lib/other/manifests/v9"); status != http.StatusNotFound {
		t.Errorf("uncached with upstream down = %d, want 404", status)
	}
}

// TestMirrorFetchesChildManifestByDigest mimics a multi-arch pull: docker
// first gets the index by tag, then requests the platform manifest by digest.
func TestMirrorFetchesChildManifestByDigest(t *testing.T) {
	child := storagetest.ManifestFor(storagetest.Digest([]byte("amd64-cfg")))
	childDgst := storagetest.Digest(child)
	index := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[{"digest":"` + childDgst + `"}]}`)

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/lib/multi/manifests/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", storagetest.Digest(index))
		_, _ = w.Write(index)
	})
	mux.HandleFunc("/v2/lib/multi/manifests/"+childDgst, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", childDgst)
		_, _ = w.Write(child)
	})
	upstream := httptest.NewServer(mux)
	t.Cleanup(upstream.Close)

	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := NewMirror(backend, events.NewHub(), registry.NewClient(upstream.URL, "", ""), time.Hour)
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)

	status, body := get(t, srv.URL+"/v2/lib/multi/manifests/latest")
	if status != http.StatusOK || !bytes.Equal(body, index) {
		t.Fatalf("index pull = %d", status)
	}
	status, body = get(t, srv.URL+"/v2/lib/multi/manifests/"+childDgst)
	if status != http.StatusOK || !bytes.Equal(body, child) {
		t.Fatalf("child-by-digest pull = %d", status)
	}

	// Cached: the child now exists locally.
	upstream.Close()
	if status, _ = get(t, srv.URL+"/v2/lib/multi/manifests/"+childDgst); status != http.StatusOK {
		t.Errorf("child not cached: %d", status)
	}
}

func TestMirrorIsAlsoAPushTarget(t *testing.T) {
	srv, _, _ := newMirrorFixture(t, time.Hour)
	content := []byte("pushed-directly")
	dgst := storagetest.Digest(content)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v2/direct/app/blobs/uploads/?digest="+dgst, bytes.NewReader(content))
	req.ContentLength = int64(len(content))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("push to mirror = %d, want 201", resp.StatusCode)
	}
	if status, body := get(t, srv.URL+"/v2/direct/app/blobs/"+dgst); status != http.StatusOK || !bytes.Equal(body, content) {
		t.Errorf("pull own push = %d", status)
	}
}
