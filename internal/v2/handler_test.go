package v2

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"dockyard/internal/events"
	"dockyard/internal/storage"
	"dockyard/internal/storage/storagetest"
)

func newTestServer(t *testing.T) (*httptest.Server, *events.Hub) {
	t.Helper()
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	hub := events.NewHub()
	srv := httptest.NewServer(New(backend, hub, nil, nil))
	t.Cleanup(srv.Close)
	return srv, hub
}

func do(t *testing.T, method, url string, body []byte) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.ContentLength = int64(len(body))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// pushBlobMonolithic pushes a blob via POST /blobs/uploads/?digest=…
func pushBlobMonolithic(t *testing.T, base, name string, content []byte) string {
	t.Helper()
	dgst := storagetest.Digest(content)
	resp := do(t, http.MethodPost, base+"/v2/"+name+"/blobs/uploads/?digest="+dgst, content)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("monolithic blob push: status = %d, want 201", resp.StatusCode)
	}
	return dgst
}

func TestPing(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := do(t, http.MethodGet, srv.URL+"/v2/", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /v2/: status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Docker-Distribution-Api-Version"); got != "registry/2.0" {
		t.Errorf("Docker-Distribution-Api-Version = %q, want registry/2.0", got)
	}
}

// TestPushPullRoundTrip walks the full docker push / docker pull sequence:
// blobs (monolithic), manifest by tag, then pull by tag and by digest.
func TestPushPullRoundTrip(t *testing.T) {
	srv, hub := newTestServer(t)
	const name = "team/app"
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	config := []byte(`{"architecture":"amd64"}`)
	layer := []byte("layer-bytes-here")
	configDgst := pushBlobMonolithic(t, srv.URL, name, config)
	layerDgst := pushBlobMonolithic(t, srv.URL, name, layer)

	manifest := storagetest.ManifestFor(configDgst, layerDgst)
	manifestDgst := storagetest.Digest(manifest)

	resp := do(t, http.MethodPut, srv.URL+"/v2/"+name+"/manifests/v1", manifest)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("manifest PUT: status = %d, want 201", resp.StatusCode)
	}
	if got := resp.Header.Get("Docker-Content-Digest"); got != manifestDgst {
		t.Errorf("manifest PUT digest header = %q, want %q", got, manifestDgst)
	}

	// The tag push must publish exactly one SSE event.
	select {
	case ev := <-sub:
		if ev.Type != "push" || ev.Name != name || ev.Tag != "v1" {
			t.Errorf("event = %+v, want push %s:v1", ev, name)
		}
	case <-time.After(2 * time.Second):
		t.Error("no push event received after tag PUT")
	}

	// Pull: manifest by tag and by digest.
	for _, ref := range []string{"v1", manifestDgst} {
		resp := do(t, http.MethodGet, srv.URL+"/v2/"+name+"/manifests/"+ref, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("manifest GET %q: status = %d, want 200", ref, resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if !bytes.Equal(body, manifest) {
			t.Errorf("manifest GET %q: content mismatch", ref)
		}
		if got := resp.Header.Get("Docker-Content-Digest"); got != manifestDgst {
			t.Errorf("manifest GET %q digest header = %q, want %q", ref, got, manifestDgst)
		}
	}
	if resp := do(t, http.MethodHead, srv.URL+"/v2/"+name+"/manifests/v1", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("manifest HEAD: status = %d, want 200", resp.StatusCode)
	}

	// Pull: blob HEAD + GET.
	if resp := do(t, http.MethodHead, srv.URL+"/v2/"+name+"/blobs/"+layerDgst, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("blob HEAD: status = %d, want 200", resp.StatusCode)
	}
	resp = do(t, http.MethodGet, srv.URL+"/v2/"+name+"/blobs/"+layerDgst, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("blob GET: status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(body, layer) {
		t.Errorf("blob GET content mismatch")
	}

	// Catalog + tags.
	resp = do(t, http.MethodGet, srv.URL+"/v2/_catalog", nil)
	var catalog struct {
		Repositories []string `json:"repositories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if !slices.Contains(catalog.Repositories, name) {
		t.Errorf("catalog = %v, missing %q", catalog.Repositories, name)
	}
	resp = do(t, http.MethodGet, srv.URL+"/v2/"+name+"/tags/list", nil)
	var tags struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		t.Fatalf("decode tags: %v", err)
	}
	if tags.Name != name || !slices.Contains(tags.Tags, "v1") {
		t.Errorf("tags/list = %+v, want name=%s containing v1", tags, name)
	}
}

func TestChunkedUpload(t *testing.T) {
	srv, _ := newTestServer(t)
	const name = "chunked/app"
	part1, part2 := []byte("chunk-one|"), []byte("chunk-two")
	full := append(append([]byte{}, part1...), part2...)
	dgst := storagetest.Digest(full)

	resp := do(t, http.MethodPost, srv.URL+"/v2/"+name+"/blobs/uploads/", nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("upload init: status = %d, want 202", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	if location == "" {
		t.Fatal("upload init: no Location header")
	}

	resp = do(t, http.MethodPatch, srv.URL+location, part1)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("PATCH part1: status = %d, want 202", resp.StatusCode)
	}
	if got := resp.Header.Get("Range"); got != "0-9" {
		t.Errorf("Range after part1 = %q, want 0-9", got)
	}
	resp = do(t, http.MethodPatch, srv.URL+location, part2)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("PATCH part2: status = %d, want 202", resp.StatusCode)
	}

	resp = do(t, http.MethodPut, srv.URL+location+"?digest="+dgst, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload commit: status = %d, want 201", resp.StatusCode)
	}

	resp = do(t, http.MethodGet, srv.URL+"/v2/"+name+"/blobs/"+dgst, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("blob GET after chunked upload: status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(body, full) {
		t.Errorf("chunked blob content = %q, want %q", body, full)
	}
}

func TestChunkedUploadCommitDigestMismatch(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := do(t, http.MethodPost, srv.URL+"/v2/app/blobs/uploads/", nil)
	location := resp.Header.Get("Location")
	do(t, http.MethodPatch, srv.URL+location, []byte("some data"))

	wrong := storagetest.Digest([]byte("different data"))
	resp = do(t, http.MethodPut, srv.URL+location+"?digest="+wrong, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("commit with wrong digest: status = %d, want 400", resp.StatusCode)
	}
}

func TestDigestPushDoesNotPublishEvent(t *testing.T) {
	srv, hub := newTestServer(t)
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	// A multi-arch child manifest is PUT by digest — no event expected.
	manifest := storagetest.ManifestFor(storagetest.Digest([]byte("cfg")))
	dgst := storagetest.Digest(manifest)
	resp := do(t, http.MethodPut, srv.URL+"/v2/app/manifests/"+dgst, manifest)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("manifest PUT by digest: status = %d, want 201", resp.StatusCode)
	}
	select {
	case ev := <-sub:
		t.Errorf("unexpected event for digest-referenced PUT: %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDeleteManifest(t *testing.T) {
	srv, _ := newTestServer(t)
	const name = "delete/app"
	manifest := storagetest.ManifestFor(storagetest.Digest([]byte("cfg")))
	dgst := storagetest.Digest(manifest)
	do(t, http.MethodPut, srv.URL+"/v2/"+name+"/manifests/v1", manifest)

	resp := do(t, http.MethodDelete, srv.URL+"/v2/"+name+"/manifests/"+dgst, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("manifest DELETE: status = %d, want 202", resp.StatusCode)
	}
	resp = do(t, http.MethodGet, srv.URL+"/v2/"+name+"/manifests/v1", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("manifest GET after delete: status = %d, want 404", resp.StatusCode)
	}
}

func TestErrorResponses(t *testing.T) {
	srv, _ := newTestServer(t)

	resp := do(t, http.MethodGet, srv.URL+"/v2/unknown/manifests/latest", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown manifest: status = %d, want 404", resp.StatusCode)
	}
	var regErr struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&regErr); err != nil {
		t.Fatalf("error body is not registry-format JSON: %v", err)
	}
	if len(regErr.Errors) == 0 || regErr.Errors[0].Code != "MANIFEST_UNKNOWN" {
		t.Errorf("error code = %+v, want MANIFEST_UNKNOWN", regErr.Errors)
	}

	missing := storagetest.Digest([]byte("no such blob"))
	if resp := do(t, http.MethodGet, srv.URL+"/v2/app/blobs/"+missing, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown blob: status = %d, want 404", resp.StatusCode)
	}
	if resp := do(t, http.MethodGet, srv.URL+"/v2/not-an-endpoint", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unsupported path: status = %d, want 404", resp.StatusCode)
	}
}

func TestIsV2Path(t *testing.T) {
	for path, want := range map[string]bool{
		"/v2":               true,
		"/v2/":              true,
		"/v2/app/tags/list": true,
		"/api/admin":        false,
		"/health":           false,
		"/v2x":              false,
	} {
		if got := IsV2Path(path); got != want {
			t.Errorf("IsV2Path(%q) = %v, want %v", path, got, want)
		}
	}
}
