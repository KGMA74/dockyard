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

// apiCall performs one request against a running test server and returns the
// status and body. token may be empty.
func apiCall(t *testing.T, base, method, path, token string, body []byte) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func createUser(t *testing.T, base, adminJWT, username, password, role string, patterns ...string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"username": username, "password": password, "role": role, "repo_patterns": patterns,
	})
	if status, raw := apiCall(t, base, http.MethodPost, "/api/admin/users", adminJWT, body); status != http.StatusCreated {
		t.Fatalf("create user %s = %d: %s", username, status, raw)
	}
}

func loginAs(t *testing.T, base, username, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	status, raw := apiCall(t, base, http.MethodPost, "/api/admin/auth/login", "", body)
	if status != http.StatusOK {
		t.Fatalf("login %s = %d: %s", username, status, raw)
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Token
}

// pushImage pushes a one-layer image to name:tag with the given registry token.
func pushImage(t *testing.T, base, token, name, tag string) {
	t.Helper()
	layer := []byte("layer of " + name + ":" + tag)
	config := []byte(`{"architecture":"amd64","os":"linux","ref":"` + name + ":" + tag + `"}`)
	for _, blob := range [][]byte{layer, config} {
		path := "/v2/" + name + "/blobs/uploads/?digest=" + storagetest.Digest(blob)
		if got := authedStatus(t, base, http.MethodPost, path, token, blob); got != http.StatusCreated {
			t.Fatalf("push blob to %s = %d", name, got)
		}
	}
	manifest := storagetest.ManifestFor(storagetest.Digest(config), storagetest.Digest(layer))
	if got := authedStatus(t, base, http.MethodPut, "/v2/"+name+"/manifests/"+tag, token, manifest); got != http.StatusCreated {
		t.Fatalf("push manifest %s:%s = %d", name, tag, got)
	}
}

// A pusher limited to team-a/* must not reach team-b by smuggling ".." into
// the repository name: "team-a/../team-b/app" matches the pattern "team-a/*"
// as a string, yet on disk it is team-b/app.
func TestPathTraversalCannotBypassRepoPatterns(t *testing.T) {
	h := newTestServer(t, func(s *Server) { s.v2AuthEnabled = true })
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	adminJWT := loginAs(t, srv.URL, "admin", "test-password")
	createUser(t, srv.URL, adminJWT, "alice", "alice-password", "pusher", "team-a/*")

	adminToken := fetchToken(t, srv.URL, "admin", "test-password")
	pushImage(t, srv.URL, adminToken, "team-b/app", "v1")

	alice := fetchToken(t, srv.URL, "alice", "alice-password")
	if got := authedStatus(t, srv.URL, http.MethodGet, "/v2/team-b/app/manifests/v1", alice, nil); got != http.StatusForbidden {
		t.Fatalf("direct access to team-b = %d, want 403", got)
	}

	evil := storagetest.ManifestFor(storagetest.Digest([]byte("x")), storagetest.Digest([]byte("y")))
	attempts := []struct{ method, path string }{
		{http.MethodGet, "/v2/team-a/../team-b/app/manifests/v1"},
		{http.MethodPut, "/v2/team-a/../team-b/app/manifests/v1"},
		{http.MethodGet, "/v2/team-a/../team-b/app/tags/list"},
		{http.MethodPost, "/v2/team-a/../team-b/app/blobs/uploads/"},
		{http.MethodGet, "/v2/team-a/%2e%2e/team-b/app/manifests/v1"},
		{http.MethodPut, `/v2/team-a/..%5Cteam-b/app/manifests/v1`},
	}
	for _, a := range attempts {
		var body []byte
		if a.method == http.MethodPut {
			body = evil
		}
		got := authedStatus(t, srv.URL, a.method, a.path, alice, body)
		if got != http.StatusBadRequest {
			t.Errorf("%s %s = %d, want 400 (malformed repository name)", a.method, a.path, got)
		}
	}

	// team-b's tag still points at what the admin pushed.
	status, body := apiCall(t, srv.URL, http.MethodGet, "/v2/team-b/app/manifests/v1", adminToken, nil)
	if status != http.StatusOK || bytes.Equal(body, evil) {
		t.Fatalf("team-b/app:v1 was tampered with (status %d)", status)
	}
}

// The admin API must show a restricted account exactly what docker pull
// would give it — no more.
func TestAdminAPIHonoursRepoPatterns(t *testing.T) {
	h := newTestServer(t, func(s *Server) { s.v2AuthEnabled = true })
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	adminJWT := loginAs(t, srv.URL, "admin", "test-password")
	adminToken := fetchToken(t, srv.URL, "admin", "test-password")
	pushImage(t, srv.URL, adminToken, "team-a/app", "v1")
	pushImage(t, srv.URL, adminToken, "team-b/secret", "v1")

	createUser(t, srv.URL, adminJWT, "alice", "alice-password", "reader", "team-a/*")
	alice := loginAs(t, srv.URL, "alice", "alice-password")

	status, raw := apiCall(t, srv.URL, http.MethodGet, "/api/admin/repositories", alice, nil)
	if status != http.StatusOK {
		t.Fatalf("list repositories = %d", status)
	}
	if !bytes.Contains(raw, []byte("team-a/app")) || bytes.Contains(raw, []byte("team-b/secret")) {
		t.Errorf("repository list leaks beyond the account's patterns: %s", raw)
	}

	status, raw = apiCall(t, srv.URL, http.MethodGet, "/api/admin/repositories/search", alice, nil)
	if status != http.StatusOK || bytes.Contains(raw, []byte("team-b/secret")) {
		t.Errorf("search (status %d) leaks beyond the account's patterns: %s", status, raw)
	}

	for _, path := range []string{
		"/api/admin/repositories/tags?name=team-b/secret",
		"/api/admin/repositories/manifest?name=team-b/secret&reference=v1",
		"/api/admin/repositories/diff?name=team-b/secret&reference_a=v1&reference_b=v1",
		"/api/admin/repositories/layer?name=team-b/secret&digest=" + storagetest.Digest([]byte("layer of team-b/secret:v1")),
	} {
		if status, _ := apiCall(t, srv.URL, http.MethodGet, path, alice, nil); status != http.StatusForbidden {
			t.Errorf("GET %s = %d, want 403", path, status)
		}
	}
	if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/repositories/tags?name=team-a/app", alice, nil); status != http.StatusOK {
		t.Errorf("tags of an allowed repository = %d, want 200", status)
	}

	// The registry catalog is filtered the same way.
	aliceRegistry := fetchToken(t, srv.URL, "alice", "alice-password")
	status, raw = apiCall(t, srv.URL, http.MethodGet, "/v2/_catalog", aliceRegistry, nil)
	if status != http.StatusOK || bytes.Contains(raw, []byte("team-b/secret")) || !bytes.Contains(raw, []byte("team-a/app")) {
		t.Errorf("catalog (status %d) = %s, want only team-a/app", status, raw)
	}

	// The raw storage tree names every repository: admins only.
	if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/storage/tree", alice, nil); status != http.StatusForbidden {
		t.Errorf("storage tree for a reader = %d, want 403", status)
	}

	// Malformed names never reach a handler.
	if status, _ := apiCall(t, srv.URL, http.MethodDelete, "/api/admin/repositories?name=..", adminJWT, nil); status != http.StatusBadRequest {
		t.Errorf("DELETE repository '..' = %d, want 400", status)
	}
	if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/repositories/tags?name=team-a/app", adminJWT, nil); status != http.StatusOK {
		t.Errorf("storage was damaged by the rejected delete (tags = %d)", status)
	}
}

// Rights are read from the database on every request: deleting or demoting
// an account, or revoking its session, takes effect on the very next call —
// not when its access token expires.
func TestAccessChangesApplyImmediately(t *testing.T) {
	h := newTestServer(t, nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	adminJWT := loginAs(t, srv.URL, "admin", "test-password")

	t.Run("deleted account", func(t *testing.T) {
		createUser(t, srv.URL, adminJWT, "bob", "bob-password", "reader")
		bob := loginAs(t, srv.URL, "bob", "bob-password")
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/repositories", bob, nil); status != http.StatusOK {
			t.Fatalf("before delete = %d, want 200", status)
		}
		if status, _ := apiCall(t, srv.URL, http.MethodDelete, "/api/admin/users/bob", adminJWT, nil); status != http.StatusOK {
			t.Fatalf("delete user = %d", status)
		}
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/repositories", bob, nil); status != http.StatusUnauthorized {
			t.Errorf("token of a deleted account = %d, want 401", status)
		}
	})

	t.Run("demoted account", func(t *testing.T) {
		createUser(t, srv.URL, adminJWT, "carol", "carol-password", "admin")
		carol := loginAs(t, srv.URL, "carol", "carol-password")
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/users", carol, nil); status != http.StatusOK {
			t.Fatalf("admin before demotion = %d, want 200", status)
		}
		demote, _ := json.Marshal(map[string]any{"role": "reader", "repo_patterns": []string{}})
		if status, raw := apiCall(t, srv.URL, http.MethodPut, "/api/admin/users/carol", adminJWT, demote); status != http.StatusOK {
			t.Fatalf("demote = %d: %s", status, raw)
		}
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/users", carol, nil); status != http.StatusForbidden {
			t.Errorf("demoted admin still reaches an admin route: %d, want 403", status)
		}
	})

	t.Run("revoked session", func(t *testing.T) {
		createUser(t, srv.URL, adminJWT, "dave", "dave-password", "reader")
		dave := loginAs(t, srv.URL, "dave", "dave-password")

		_, raw := apiCall(t, srv.URL, http.MethodGet, "/api/admin/sessions", adminJWT, nil)
		var list struct {
			Sessions []struct {
				ID       int64  `json:"id"`
				Username string `json:"username"`
			} `json:"sessions"`
		}
		if err := json.Unmarshal(raw, &list); err != nil {
			t.Fatal(err)
		}
		for _, s := range list.Sessions {
			if s.Username == "dave" {
				apiCall(t, srv.URL, http.MethodDelete, fmt.Sprintf("/api/admin/sessions/%d", s.ID), adminJWT, nil)
			}
		}
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/repositories", dave, nil); status != http.StatusUnauthorized {
			t.Errorf("access token of a revoked session = %d, want 401", status)
		}
	})

	t.Run("password change ends other sessions", func(t *testing.T) {
		createUser(t, srv.URL, adminJWT, "erin", "erin-password", "reader")
		stolen := loginAs(t, srv.URL, "erin", "erin-password")
		mine := loginAs(t, srv.URL, "erin", "erin-password")

		change, _ := json.Marshal(map[string]string{"current_password": "erin-password", "new_password": "erin-new-password"})
		if status, raw := apiCall(t, srv.URL, http.MethodPost, "/api/admin/auth/password", mine, change); status != http.StatusOK {
			t.Fatalf("change password = %d: %s", status, raw)
		}
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/repositories", mine, nil); status != http.StatusOK {
			t.Errorf("the session that changed the password was logged out (%d)", status)
		}
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/repositories", stolen, nil); status != http.StatusUnauthorized {
			t.Errorf("another session survived the password change: %d, want 401", status)
		}
	})

	t.Run("reserved and malformed usernames", func(t *testing.T) {
		for _, name := range []string{"anonymous", "Anonymous", "", "with space", "a/b"} {
			body, _ := json.Marshal(map[string]any{"username": name, "password": "long-enough-password", "role": "admin"})
			if status, _ := apiCall(t, srv.URL, http.MethodPost, "/api/admin/users", adminJWT, body); status != http.StatusBadRequest {
				t.Errorf("create user %q = %d, want 400", name, status)
			}
		}
	})
}

// X-Forwarded-For is client input. From an untrusted (public) peer it must
// not change which bucket the login rate limiter charges.
func TestSpoofedForwardedForDoesNotBypassLoginRateLimit(t *testing.T) {
	attempt := func(h http.Handler, remoteAddr, forwardedFor string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login",
			strings.NewReader(`{"username":"admin","password":"wrong"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", forwardedFor)
		req.RemoteAddr = remoteAddr
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	t.Run("public peer: header ignored", func(t *testing.T) {
		h := newTestServer(t, func(s *Server) { s.rateLimitLoginPerMin = 3 })
		last := 0
		for i := range 8 {
			last = attempt(h, "203.0.113.7:4000", fmt.Sprintf("198.51.100.%d", i))
		}
		if last != http.StatusTooManyRequests {
			t.Errorf("8th attempt with a rotating fake X-Forwarded-For = %d, want 429", last)
		}
	})

	t.Run("trusted proxy: header honoured", func(t *testing.T) {
		h := newTestServer(t, func(s *Server) {
			s.rateLimitLoginPerMin = 3
			s.trustedProxies = []string{"203.0.113.0/24"}
		})
		for i := range 8 {
			if got := attempt(h, "203.0.113.7:4000", fmt.Sprintf("198.51.100.%d", i)); got != http.StatusUnauthorized {
				t.Fatalf("attempt %d through a trusted proxy = %d, want 401 (distinct clients)", i, got)
			}
		}
	})

	t.Run("explicit list replaces the private-network default", func(t *testing.T) {
		h := newTestServer(t, func(s *Server) {
			s.rateLimitLoginPerMin = 3
			s.trustedProxies = []string{"203.0.113.7"}
		})
		last := 0
		for i := range 8 {
			last = attempt(h, "10.0.0.5:4000", fmt.Sprintf("198.51.100.%d", i))
		}
		if last != http.StatusTooManyRequests {
			t.Errorf("private peer not in TRUSTED_PROXIES was believed: %d, want 429", last)
		}
	})
}

func TestClientIPExtractorRejectsGarbage(t *testing.T) {
	if _, err := clientIPExtractor([]string{"not-an-ip"}); err == nil {
		t.Error("an invalid TRUSTED_PROXIES entry was accepted")
	}
	if _, err := clientIPExtractor([]string{"10.0.0.0/8", "2001:db8::1", "192.168.1.10"}); err != nil {
		t.Errorf("valid entries rejected: %v", err)
	}
}

func TestRedactURI(t *testing.T) {
	cases := map[string]string{
		"/api/admin/events?token=eyJhbGciOi.secret.sig": "/api/admin/events?token=REDACTED",
		"/api/admin/repositories/tags?name=team/app":    "/api/admin/repositories/tags?name=team/app",
		"/v2/":                           "/v2/",
		"/x?a=1&Token=abc":               "/x?Token=REDACTED&a=1",
		"/x?refresh_token=abc&name=y":    "/x?name=y&refresh_token=REDACTED",
		"/api/admin/events?token=abc%zz": "/api/admin/events?[unparseable query redacted]",
		"/api/admin/repositories/search?q=token&limit=50": "/api/admin/repositories/search?q=token&limit=50",
	}
	for in, want := range cases {
		if got := redactURI(in); got != want {
			t.Errorf("redactURI(%q) = %q, want %q", in, got, want)
		}
	}
	if strings.Contains(redactURI("/api/admin/events?token=super-secret-jwt"), "super-secret-jwt") {
		t.Error("token survived redaction")
	}
}

func TestSecurityHeadersAndStaticRouting(t *testing.T) {
	h := newTestServer(t, nil)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	for _, path := range []string{"/", "/images/team/app", "/health"} {
		rec := get(path)
		for header, want := range map[string]string{
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "DENY",
			"Referrer-Policy":        "no-referrer",
		} {
			if got := rec.Header().Get(header); got != want {
				t.Errorf("GET %s: %s = %q, want %q", path, header, got, want)
			}
		}
		csp := rec.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") {
			t.Errorf("GET %s: weak or missing CSP: %q", path, csp)
		}
	}

	// Registry traffic is for container runtimes — no browser headers there.
	if csp := get("/v2/").Header().Get("Content-Security-Policy"); csp != "" {
		t.Errorf("/v2/ carries a CSP: %q", csp)
	}

	// The SPA shell must be revalidated, or a deploy keeps serving the old UI.
	if cc := get("/").Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("index Cache-Control = %q, want no-cache", cc)
	}

	// Unknown API paths are a JSON 404, not the SPA shell with a 200.
	rec := get("/api/admin/does-not-exist-and-is-not-under-auth/../../nope")
	if rec := get("/api/nope"); rec.Code != http.StatusNotFound || !strings.Contains(rec.Header().Get("Content-Type"), "json") {
		t.Errorf("GET /api/nope = %d %q, want a JSON 404", rec.Code, rec.Header().Get("Content-Type"))
	}
	_ = rec
}
