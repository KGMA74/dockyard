package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

func requestV2Token(t *testing.T, m *Manager, anonymousPull bool, basicUser, basicPass string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/v2/token?service=dockyard-registry&scope=repository:team/app:pull,push", nil)
	if basicUser != "" {
		req.SetBasicAuth(basicUser, basicPass)
	}
	rec := httptest.NewRecorder()
	if err := m.V2Token(anonymousPull)(e.NewContext(req, rec)); err != nil {
		t.Fatalf("V2Token: %v", err)
	}
	var tok string
	if rec.Code == http.StatusOK {
		var resp struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode token response: %v", err)
		}
		tok = resp.Token
	}
	return rec, tok
}

func TestV2TokenEndpoint(t *testing.T) {
	m := newManager(t)

	rec, tok := requestV2Token(t, m, false, testUser, testPassword)
	if rec.Code != http.StatusOK || tok == "" {
		t.Fatalf("valid creds: status = %d, token empty=%v", rec.Code, tok == "")
	}
	p, err := m.validate(tok)
	if err != nil || p.Role != store.RoleAdmin || p.Username != testUser {
		t.Errorf("registry token principal = %+v, %v", p, err)
	}

	if rec, _ := requestV2Token(t, m, false, testUser, "wrong"); rec.Code != http.StatusUnauthorized {
		t.Errorf("bad creds: status = %d, want 401", rec.Code)
	}
	if rec, _ := requestV2Token(t, m, false, "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no creds, anon off: status = %d, want 401", rec.Code)
	}

	rec, tok = requestV2Token(t, m, true, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("no creds, anon on: status = %d, want 200", rec.Code)
	}
	p, err = m.validate(tok)
	if err != nil || p.Role != store.RoleReader || p.Username != anonymousUser {
		t.Errorf("anonymous token = %+v, %v; want reader/anonymous", p, err)
	}
}

// v2Backend returns a middleware-wrapped handler that records whether the
// request got through.
func v2Backend(m *Manager, anonymousPull bool) (http.Handler, *bool) {
	passed := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		passed = true
		w.WriteHeader(http.StatusOK)
	})
	return m.V2Middleware(anonymousPull)(inner), &passed
}

func TestV2MiddlewareChallenge(t *testing.T) {
	m := newManager(t)
	h, passed := v2Backend(m, false)

	req := httptest.NewRequest(http.MethodGet, "/v2/team/app/manifests/latest", nil)
	req.Host = "registry.example.com"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized || *passed {
		t.Fatalf("unauthenticated: status = %d passed=%v, want 401", rec.Code, *passed)
	}
	challenges := rec.Header().Values("WWW-Authenticate")
	joined := strings.Join(challenges, " | ")
	if !strings.Contains(joined, `Bearer realm="http://registry.example.com/v2/token"`) {
		t.Errorf("missing Bearer realm challenge: %s", joined)
	}
	if !strings.Contains(joined, `scope="repository:team/app:pull"`) {
		t.Errorf("missing scope in challenge: %s", joined)
	}
	if !strings.Contains(joined, "Basic realm=") {
		t.Errorf("missing Basic fallback challenge: %s", joined)
	}
}

func TestV2MiddlewareRBAC(t *testing.T) {
	m := newManager(t)
	// A pusher restricted to team-a/*.
	if _, err := m.users.CreateUser("pusher1", mustHash(t, "pusher-pass"), store.RolePusher, []string{"team-a/*"}); err != nil {
		t.Fatal(err)
	}
	// A reader with no restriction.
	if _, err := m.users.CreateUser("reader1", mustHash(t, "reader-pass"), store.RoleReader, nil); err != nil {
		t.Fatal(err)
	}

	tokenFor := func(username, password string) string {
		t.Helper()
		rec, tok := requestV2Token(t, m, false, username, password)
		if rec.Code != http.StatusOK {
			t.Fatalf("token for %s: %d", username, rec.Code)
		}
		return tok
	}

	cases := []struct {
		name   string
		token  string
		method string
		path   string
		want   int
	}{
		{"reader pulls", tokenFor("reader1", "reader-pass"), http.MethodGet, "/v2/team-a/app/manifests/v1", http.StatusOK},
		{"reader push denied", tokenFor("reader1", "reader-pass"), http.MethodPut, "/v2/team-a/app/manifests/v1", http.StatusForbidden},
		{"pusher pushes own repo", tokenFor("pusher1", "pusher-pass"), http.MethodPut, "/v2/team-a/app/manifests/v1", http.StatusOK},
		{"pusher push other repo denied", tokenFor("pusher1", "pusher-pass"), http.MethodPut, "/v2/team-b/app/manifests/v1", http.StatusForbidden},
		{"pusher delete denied", tokenFor("pusher1", "pusher-pass"), http.MethodDelete, "/v2/team-a/app/manifests/sha256:abc", http.StatusForbidden},
		{"admin deletes", tokenFor(testUser, testPassword), http.MethodDelete, "/v2/team-a/app/manifests/sha256:abc", http.StatusOK},
		{"ping with token", tokenFor("reader1", "reader-pass"), http.MethodGet, "/v2/", http.StatusOK},
	}
	for _, tc := range cases {
		h, _ := v2Backend(m, false)
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d (%s)", tc.name, rec.Code, tc.want, rec.Body.String())
		}
	}

	// Basic fallback works end to end (plain docker login without token flow).
	h, _ := v2Backend(m, false)
	req := httptest.NewRequest(http.MethodPut, "/v2/team-a/app/manifests/v1", nil)
	req.SetBasicAuth("pusher1", "pusher-pass")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("basic fallback push: status = %d, want 200", rec.Code)
	}
}

func TestV2MiddlewareAnonymousPull(t *testing.T) {
	m := newManager(t)

	h, _ := v2Backend(m, true)
	req := httptest.NewRequest(http.MethodGet, "/v2/public/app/manifests/latest", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("anonymous pull with V2_ANONYMOUS_PULL: status = %d, want 200", rec.Code)
	}

	h, passed := v2Backend(m, true)
	req = httptest.NewRequest(http.MethodPut, "/v2/public/app/manifests/latest", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || *passed {
		t.Errorf("anonymous push: status = %d, want 401", rec.Code)
	}
}

func TestV2Scope(t *testing.T) {
	cases := []struct {
		method, path string
		action       Action
		repo         string
	}{
		{http.MethodGet, "/v2/", ActionPull, ""},
		{http.MethodGet, "/v2/_catalog", ActionPull, ""},
		{http.MethodGet, "/v2/org/app/tags/list", ActionPull, "org/app"},
		{http.MethodHead, "/v2/org/sub/app/manifests/latest", ActionPull, "org/sub/app"},
		{http.MethodPut, "/v2/app/manifests/v1", ActionPush, "app"},
		{http.MethodDelete, "/v2/app/manifests/sha256:abc", ActionDelete, "app"},
		{http.MethodGet, "/v2/app/blobs/sha256:0a1b", ActionPull, "app"},
		{http.MethodPost, "/v2/app/blobs/uploads/", ActionPush, "app"},
		{http.MethodPatch, "/v2/app/blobs/uploads/uuid-1", ActionPush, "app"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		action, repo := v2Scope(req)
		if action != tc.action || repo != tc.repo {
			t.Errorf("v2Scope(%s %s) = (%s, %q), want (%s, %q)",
				tc.method, tc.path, action, repo, tc.action, tc.repo)
		}
	}
}

func mustHash(t *testing.T, password string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(h)
}
