package registry

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
