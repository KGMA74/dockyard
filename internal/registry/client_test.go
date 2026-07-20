package registry

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// newTokenProtectedUpstream simulates a Docker-Hub-style registry: requests
// without a Bearer token get a 401 challenge pointing at an auth endpoint.
func newTokenProtectedUpstream(t *testing.T) (upstream *httptest.Server, tokenRequests, dataRequests *atomic.Int64) {
	t.Helper()
	tokenRequests = &atomic.Int64{}
	dataRequests = &atomic.Int64{}

	mux := http.NewServeMux()
	var authURL string

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		tokenRequests.Add(1)
		if r.URL.Query().Get("scope") != "repository:library/alpine:pull" {
			http.Error(w, "bad scope", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"token":"granted-token","expires_in":300}`)
	})
	mux.HandleFunc("/v2/library/alpine/manifests/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer granted-token" {
			w.Header().Set("Www-Authenticate",
				fmt.Sprintf(`Bearer realm=%q,service="test-registry",scope="repository:library/alpine:pull"`, authURL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		dataRequests.Add(1)
		w.Header().Set("Docker-Content-Digest", "sha256:feedface")
		_, _ = w.Write([]byte(`{"schemaVersion":2}`))
	})

	upstream = httptest.NewServer(mux)
	t.Cleanup(upstream.Close)
	authURL = upstream.URL + "/token"
	return upstream, tokenRequests, dataRequests
}

func TestBearerTokenDance(t *testing.T) {
	upstream, tokenReqs, dataReqs := newTokenProtectedUpstream(t)
	c := NewClient(upstream.URL, "", "")

	raw, digest, err := c.RawManifest("library/alpine", "latest")
	if err != nil {
		t.Fatalf("RawManifest through token dance: %v", err)
	}
	if digest != "sha256:feedface" || len(raw) == 0 {
		t.Errorf("manifest = %q digest = %q", raw, digest)
	}
	if tokenReqs.Load() != 1 || dataReqs.Load() != 1 {
		t.Errorf("token=%d data=%d requests, want 1/1", tokenReqs.Load(), dataReqs.Load())
	}

	// Second call: the scoped token is cached, no second dance.
	if _, _, err := c.RawManifest("library/alpine", "latest"); err != nil {
		t.Fatal(err)
	}
	if tokenReqs.Load() != 1 {
		t.Errorf("token endpoint hit again despite cache: %d", tokenReqs.Load())
	}
	if dataReqs.Load() != 2 {
		t.Errorf("data requests = %d, want 2", dataReqs.Load())
	}
}

func TestPlainBasicStillWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "u" || pass != "p" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(srv.URL, "u", "p")
	if _, _, err := c.RawManifest("app", "v1"); err != nil {
		t.Fatalf("basic auth request: %v", err)
	}

	bad := NewClient(srv.URL, "u", "wrong")
	if _, _, err := bad.RawManifest("app", "v1"); err == nil {
		t.Fatal("bad basic credentials accepted")
	}
}

// pushMockRegistry simulates just enough of the V2 push surface for the
// replication client tests: HEAD blob existence, monolithic blob upload,
// and manifest PUT. Not auth-protected — TestPushRetriesBodyAfterTokenChallenge
// covers the 401 retry path separately with its own mux.
func pushMockRegistry(t *testing.T) (srv *httptest.Server, blobs map[string][]byte, manifests map[string][]byte) {
	t.Helper()
	blobs = map[string][]byte{}
	manifests = map[string][]byte{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/app/blobs/", func(w http.ResponseWriter, r *http.Request) {
		digest := strings.TrimPrefix(r.URL.Path, "/v2/app/blobs/")
		switch r.Method {
		case http.MethodHead:
			if _, ok := blobs[digest]; ok {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		case http.MethodPost:
			digest = r.URL.Query().Get("digest")
			body, _ := io.ReadAll(r.Body)
			blobs[digest] = body
			w.WriteHeader(http.StatusCreated)
		}
	})
	mux.HandleFunc("/v2/app/manifests/", func(w http.ResponseWriter, r *http.Request) {
		ref := strings.TrimPrefix(r.URL.Path, "/v2/app/manifests/")
		body, _ := io.ReadAll(r.Body)
		manifests[ref] = body
		w.WriteHeader(http.StatusCreated)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, blobs, manifests
}

func TestHasBlob(t *testing.T) {
	srv, blobs, _ := pushMockRegistry(t)
	blobs["sha256:existing"] = []byte("x")
	c := NewClient(srv.URL, "", "")

	has, err := c.HasBlob("app", "sha256:existing")
	if err != nil || !has {
		t.Fatalf("HasBlob(existing) = %v, %v", has, err)
	}
	has, err = c.HasBlob("app", "sha256:missing")
	if err != nil || has {
		t.Fatalf("HasBlob(missing) = %v, %v", has, err)
	}
}

func TestPushBlobSkipsExistingContent(t *testing.T) {
	srv, blobs, _ := pushMockRegistry(t)
	blobs["sha256:existing"] = []byte("already there")
	c := NewClient(srv.URL, "", "")

	opened := false
	err := c.PushBlob("app", "sha256:existing", 5, func() (io.ReadCloser, error) {
		opened = true
		return io.NopCloser(strings.NewReader("hello")), nil
	})
	if err != nil {
		t.Fatalf("PushBlob: %v", err)
	}
	if opened {
		t.Error("PushBlob re-uploaded a blob the target already has")
	}
}

func TestPushBlobUploadsMissingContent(t *testing.T) {
	srv, blobs, _ := pushMockRegistry(t)
	c := NewClient(srv.URL, "", "")

	content := []byte("new blob content")
	err := c.PushBlob("app", "sha256:new", int64(len(content)), func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(content)), nil
	})
	if err != nil {
		t.Fatalf("PushBlob: %v", err)
	}
	if string(blobs["sha256:new"]) != "new blob content" {
		t.Fatalf("blob not uploaded correctly: %q", blobs["sha256:new"])
	}
}

func TestPushManifest(t *testing.T) {
	srv, _, manifests := pushMockRegistry(t)
	c := NewClient(srv.URL, "", "")

	content := []byte(`{"schemaVersion":2}`)
	if err := c.PushManifest("app", "v1", "application/vnd.docker.distribution.manifest.v2+json", content); err != nil {
		t.Fatalf("PushManifest: %v", err)
	}
	if string(manifests["v1"]) != string(content) {
		t.Fatalf("manifest not pushed correctly: %q", manifests["v1"])
	}
}

// TestPushRetriesBodyAfterTokenChallenge verifies doBody re-invokes open()
// for the retry after a 401 — the first body reader was already consumed by
// the rejected attempt, so reusing it would send an empty/truncated body.
func TestPushRetriesBodyAfterTokenChallenge(t *testing.T) {
	var authURL string
	var authorized atomic.Bool
	var received []byte

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		authorized.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"token":"granted","expires_in":300}`)
	})
	mux.HandleFunc("/v2/app/manifests/v1", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer granted" {
			w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm=%q,service="s",scope="repository:app:push"`, authURL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	authURL = srv.URL + "/token"

	c := NewClient(srv.URL, "", "")
	content := []byte(`{"schemaVersion":2}`)
	if err := c.PushManifest("app", "v1", "application/vnd.docker.distribution.manifest.v2+json", content); err != nil {
		t.Fatalf("PushManifest through token challenge: %v", err)
	}
	if string(received) != string(content) {
		t.Fatalf("target received %q, want the full manifest body %q", received, content)
	}
}

func TestRepoHelpers(t *testing.T) {
	if got := repoFromPath("/v2/library/alpine/manifests/latest"); got != "library/alpine" {
		t.Errorf("repoFromPath = %q", got)
	}
	if got := repoFromPath("/v2/org/sub/app/blobs/sha256:abc"); got != "org/sub/app" {
		t.Errorf("repoFromPath nested = %q", got)
	}
	if got := repoFromPath("/v2/_catalog"); got != "" {
		t.Errorf("repoFromPath catalog = %q", got)
	}
	if got := repoFromScope("repository:library/alpine:pull"); got != "library/alpine" {
		t.Errorf("repoFromScope = %q", got)
	}
}
