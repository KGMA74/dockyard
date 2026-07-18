package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dockyard/internal/storage/storagetest"
)

// TestDockerTokenAuthFlow walks the exact sequence a docker client performs
// against the full route stack: probe /v2/ → 401 with a Bearer challenge →
// fetch a token from the advertised realm with Basic credentials → push and
// pull with the Bearer token.
func TestDockerTokenAuthFlow(t *testing.T) {
	h := newTestServer(t, func(s *Server) { s.v2AuthEnabled = true })
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	// 1. Unauthenticated ping → 401 + challenge.
	resp, err := http.Get(srv.URL + "/v2/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ping = %d, want 401", resp.StatusCode)
	}
	challenge := strings.Join(resp.Header.Values("WWW-Authenticate"), " ")
	if !strings.Contains(challenge, "/v2/token") {
		t.Fatalf("challenge missing token realm: %s", challenge)
	}

	// 2. Trade Basic credentials for a token at the advertised endpoint.
	token := fetchToken(t, srv.URL, "admin", "test-password")

	// 3. Ping again with the token.
	if got := authedStatus(t, srv.URL, http.MethodGet, "/v2/", token, nil); got != http.StatusOK {
		t.Fatalf("authed ping = %d, want 200", got)
	}

	// 4. Push: monolithic blob + manifest by tag.
	config := []byte(`{"arch":"amd64"}`)
	configDgst := storagetest.Digest(config)
	if got := authedStatus(t, srv.URL, http.MethodPost,
		"/v2/team/app/blobs/uploads/?digest="+configDgst, token, config); got != http.StatusCreated {
		t.Fatalf("blob push = %d, want 201", got)
	}
	manifest := storagetest.ManifestFor(configDgst)
	if got := authedStatus(t, srv.URL, http.MethodPut,
		"/v2/team/app/manifests/v1", token, manifest); got != http.StatusCreated {
		t.Fatalf("manifest push = %d, want 201", got)
	}

	// 5. Pull it back.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v2/team/app/manifests/v1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, manifest) {
		t.Fatalf("pull = %d, content match=%v", resp.StatusCode, bytes.Equal(body, manifest))
	}
}

// TestV2RBACThroughFullStack: a reader account created via the admin API can
// pull but not push, all through real HTTP routing.
func TestV2RBACThroughFullStack(t *testing.T) {
	h := newTestServer(t, func(s *Server) { s.v2AuthEnabled = true })
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	adminJWT := loginJSON(t, srv.URL, "admin", "test-password")

	// Create the reader through the admin API.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/admin/users",
		strings.NewReader(`{"username":"reader1","password":"reader-pass-1","role":"reader"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminJWT)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create reader = %d", resp.StatusCode)
	}

	readerToken := fetchToken(t, srv.URL, "reader1", "reader-pass-1")

	if got := authedStatus(t, srv.URL, http.MethodGet, "/v2/_catalog", readerToken, nil); got != http.StatusOK {
		t.Errorf("reader catalog = %d, want 200", got)
	}
	blob := []byte("nope")
	if got := authedStatus(t, srv.URL, http.MethodPost,
		"/v2/x/blobs/uploads/?digest="+storagetest.Digest(blob), readerToken, blob); got != http.StatusForbidden {
		t.Errorf("reader push = %d, want 403", got)
	}
}

// TestRevokedSessionCannotRefreshThroughStack: login, revoke via the admin
// endpoint, then the refresh token must be dead.
func TestRevokedSessionCannotRefreshThroughStack(t *testing.T) {
	h := newTestServer(t, nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	// Login twice: victim + admin session for the revocation call.
	victim := loginFull(t, srv.URL)
	admin := loginFull(t, srv.URL)

	// List sessions, revoke the victim's (the one that is not current).
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/admin/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+admin.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Sessions  []struct{ ID int64 }
		CurrentID int64 `json:"current_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	var victimID int64
	for _, s := range list.Sessions {
		if s.ID != list.CurrentID {
			victimID = s.ID
		}
	}
	req, _ = http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/admin/sessions/%d", srv.URL, victimID), nil)
	req.Header.Set("Authorization", "Bearer "+admin.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke session = %d", resp.StatusCode)
	}

	// The victim's refresh token is now dead.
	resp, err = http.Post(srv.URL+"/api/admin/auth/refresh", "application/json",
		strings.NewReader(`{"refresh_token":"`+victim.RefreshToken+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("refresh after revoke = %d, want 401", resp.StatusCode)
	}
	// The admin's session still refreshes fine.
	resp, err = http.Post(srv.URL+"/api/admin/auth/refresh", "application/json",
		strings.NewReader(`{"refresh_token":"`+admin.RefreshToken+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("surviving session refresh = %d, want 200", resp.StatusCode)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func fetchToken(t *testing.T, base, username, password string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+"/v2/token?service=dockyard-registry", nil)
	req.SetBasicAuth(username, password)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token endpoint = %d", resp.StatusCode)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Token
}

func authedStatus(t *testing.T, base, method, path, token string, body []byte) int {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.ContentLength = int64(len(body))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode
}

type loginResp struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

func loginFull(t *testing.T, base string) loginResp {
	t.Helper()
	resp, err := http.Post(base+"/api/admin/auth/login", "application/json",
		strings.NewReader(`{"username":"admin","password":"test-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d", resp.StatusCode)
	}
	var body loginResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func loginJSON(t *testing.T, base, _, _ string) string {
	t.Helper()
	return loginFull(t, base).Token
}
