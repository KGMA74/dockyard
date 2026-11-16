package v2

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dockyard/internal/auth"
	"dockyard/internal/storage"
	"dockyard/internal/storage/storagetest"
	"dockyard/internal/store"
)

func errorCode(t *testing.T, resp *http.Response) string {
	t.Helper()
	var body struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &body); err != nil || len(body.Errors) == 0 {
		t.Fatalf("not a registry error body: %s", raw)
	}
	return body.Errors[0].Code
}

func TestMalformedReferencesAreRejected(t *testing.T) {
	srv, _ := newTestServer(t)
	valid := storagetest.Digest([]byte("x"))

	cases := []struct {
		method, path, wantCode string
	}{
		{http.MethodGet, "/v2/../outside/manifests/latest", "NAME_INVALID"},
		{http.MethodGet, "/v2/team-a/../team-b/tags/list", "NAME_INVALID"},
		{http.MethodPut, "/v2/a//b/manifests/latest", "NAME_INVALID"},
		{http.MethodGet, `/v2/a%5Cb/manifests/latest`, "NAME_INVALID"},
		{http.MethodGet, "/v2/app/manifests/..", "TAG_INVALID"},
		{http.MethodGet, "/v2/app/manifests/sha256:abc", "DIGEST_INVALID"},
		// Used to panic (index out of range in blobPath) and surface as a 500.
		{http.MethodGet, "/v2/app/blobs/sha256:a", "DIGEST_INVALID"},
		{http.MethodPatch, "/v2/app/blobs/uploads/..", "BLOB_UPLOAD_INVALID"},
		{http.MethodPut, "/v2/app/blobs/uploads/..?digest=" + valid, "BLOB_UPLOAD_INVALID"},
	}
	for _, tc := range cases {
		resp := do(t, tc.method, srv.URL+tc.path, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s %s = %d, want 400", tc.method, tc.path, resp.StatusCode)
			continue
		}
		if got := errorCode(t, resp); got != tc.wantCode {
			t.Errorf("%s %s: code = %s, want %s", tc.method, tc.path, got, tc.wantCode)
		}
	}

	// Digest query params are validated too.
	resp := do(t, http.MethodPost, srv.URL+"/v2/app/blobs/uploads/?digest=sha256:../../x", []byte("x"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("monolithic upload with a traversal digest = %d, want 400", resp.StatusCode)
	}
}

func TestManifestPushIsValidated(t *testing.T) {
	srv, _ := newTestServer(t)
	manifest := storagetest.ManifestFor(storagetest.Digest([]byte("cfg")), storagetest.Digest([]byte("layer")))
	real := storagetest.Digest(manifest)

	// By digest: the body must hash to the digest in the URL.
	other := storagetest.Digest([]byte("some other manifest"))
	resp := do(t, http.MethodPut, srv.URL+"/v2/app/manifests/"+other, manifest)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, resp) != "DIGEST_INVALID" {
		t.Errorf("manifest pushed under a digest it does not hash to: status %d", resp.StatusCode)
	}
	if resp := do(t, http.MethodPut, srv.URL+"/v2/app/manifests/"+real, manifest); resp.StatusCode != http.StatusCreated {
		t.Errorf("manifest pushed under its own digest = %d, want 201", resp.StatusCode)
	}

	// Not JSON.
	if resp := do(t, http.MethodPut, srv.URL+"/v2/app/manifests/latest", []byte("not json at all")); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("non-JSON manifest = %d, want 400", resp.StatusCode)
	}

	// Oversized: manifests are buffered in memory, so the body is capped.
	huge := bytes.Repeat([]byte(" "), maxManifestBytes+1)
	if resp := do(t, http.MethodPut, srv.URL+"/v2/app/manifests/latest", huge); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized manifest = %d, want 413", resp.StatusCode)
	}
}

// One bad upload must not take down a blob other images depend on.
func TestBadBlobUploadDoesNotDestroyExistingBlob(t *testing.T) {
	srv, _ := newTestServer(t)
	good := []byte("base layer shared by every image")
	dgst := pushBlobMonolithic(t, srv.URL, "team/base", good)

	resp := do(t, http.MethodPost, srv.URL+"/v2/attacker/app/blobs/uploads/?digest="+dgst, []byte("garbage"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("mismatching upload = %d, want 400", resp.StatusCode)
	}

	resp = do(t, http.MethodGet, srv.URL+"/v2/team/base/blobs/"+dgst, nil)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, good) {
		t.Fatalf("existing blob damaged by someone else's bad upload: status %d, %d bytes", resp.StatusCode, len(body))
	}
}

func TestCatalogIsFilteredByRepoPatterns(t *testing.T) {
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manifest := storagetest.ManifestFor(storagetest.Digest([]byte("cfg")), storagetest.Digest([]byte("layer")))
	for _, repo := range []string{"team-a/app", "team-a/tools/cli", "team-b/secret"} {
		if err := backend.PutManifest(repo, "v1", storagetest.Digest(manifest), manifest); err != nil {
			t.Fatal(err)
		}
	}
	h := New(backend, nil, nil, nil)

	catalog := func(p *auth.Principal) string {
		req := httptest.NewRequest(http.MethodGet, "/v2/_catalog", nil)
		if p != nil {
			req = auth.WithPrincipal(req, *p)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Body.String()
	}

	restricted := catalog(&auth.Principal{Username: "alice", Role: store.RoleReader, RepoPatterns: []string{"team-a/*"}})
	if strings.Contains(restricted, "team-b/secret") || !strings.Contains(restricted, "team-a/app") || !strings.Contains(restricted, "team-a/tools/cli") {
		t.Errorf("restricted catalog = %s", restricted)
	}
	if all := catalog(&auth.Principal{Username: "root", Role: store.RoleAdmin}); !strings.Contains(all, "team-b/secret") {
		t.Errorf("unrestricted catalog is missing repositories: %s", all)
	}
	// With /v2 auth disabled there is no principal: everything is listed.
	if open := catalog(nil); !strings.Contains(open, "team-b/secret") {
		t.Errorf("open-registry catalog is missing repositories: %s", open)
	}
}
