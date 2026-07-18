package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"dockyard/internal/store"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

const (
	testUser     = "admin"
	testPassword = "correct-horse"
	testSecret   = "test-jwt-secret"
)

func newManagerIn(t *testing.T, dir string) *Manager {
	t.Helper()
	st, err := store.Open(filepath.Join(dir, "dockyard.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	m, err := New(testUser, testPassword, testSecret, "", dir, st)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	return m
}

func newManager(t *testing.T) *Manager {
	t.Helper()
	return newManagerIn(t, t.TempDir())
}

type authResp struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	Role         string `json:"role"`
	ExpiresIn    int    `json:"expires_in"`
}

func doLoginFull(t *testing.T, m *Manager, username, password string) (*httptest.ResponseRecorder, authResp) {
	t.Helper()
	e := echo.New()
	body := `{"username":"` + username + `","password":"` + password + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	if err := m.Login(e.NewContext(req, rec)); err != nil {
		t.Fatalf("Login handler: %v", err)
	}
	var resp authResp
	if rec.Code == http.StatusOK {
		decodeJSON(t, rec, &resp)
	}
	return rec, resp
}

func doLogin(t *testing.T, m *Manager, username, password string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	rec, resp := doLoginFull(t, m, username, password)
	return rec, resp.Token
}

func doRefresh(t *testing.T, m *Manager, refreshToken string) (*httptest.ResponseRecorder, authResp) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/refresh",
		strings.NewReader(`{"refresh_token":"`+refreshToken+`"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	if err := m.Refresh(e.NewContext(req, rec)); err != nil {
		t.Fatalf("Refresh handler: %v", err)
	}
	var resp authResp
	if rec.Code == http.StatusOK {
		decodeJSON(t, rec, &resp)
	}
	return rec, resp
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
}

// callProtected sends a request through m.Middleware() and reports the status.
func callProtected(t *testing.T, m *Manager, authHeader string) int {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/repositories", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	next := func(c echo.Context) error { return c.NoContent(http.StatusOK) }
	if err := m.Middleware()(next)(e.NewContext(req, rec)); err != nil {
		t.Fatalf("middleware: %v", err)
	}
	return rec.Code
}

func TestLoginIssuesValidJWT(t *testing.T) {
	m := newManager(t)
	rec, token := doLogin(t, m, testUser, testPassword)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	tok, err := m.parseToken(token)
	if err != nil || !tok.Valid {
		t.Fatalf("issued token does not parse: %v", err)
	}
	sub, _ := tok.Claims.GetSubject()
	if sub != testUser {
		t.Errorf("sub claim = %q, want %q", sub, testUser)
	}
	exp, err := tok.Claims.GetExpirationTime()
	if err != nil || exp == nil {
		t.Fatalf("token has no expiry: %v", err)
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	m := newManager(t)
	for name, creds := range map[string][2]string{
		"wrong password": {testUser, "nope"},
		"wrong username": {"eve", testPassword},
		"both empty":     {"", ""},
	} {
		rec, _ := doLogin(t, m, creds[0], creds[1])
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", name, rec.Code)
		}
	}
}

func TestMiddleware(t *testing.T) {
	m := newManager(t)
	_, token := doLogin(t, m, testUser, testPassword)

	if got := callProtected(t, m, "Bearer "+token); got != http.StatusOK {
		t.Errorf("valid token: status = %d, want 200", got)
	}
	if got := callProtected(t, m, ""); got != http.StatusUnauthorized {
		t.Errorf("missing header: status = %d, want 401", got)
	}
	if got := callProtected(t, m, "Bearer garbage"); got != http.StatusUnauthorized {
		t.Errorf("garbage token: status = %d, want 401", got)
	}

	// Token signed with a different secret must be rejected.
	forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": testUser}).
		SignedString([]byte("other-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if got := callProtected(t, m, "Bearer "+forged); got != http.StatusUnauthorized {
		t.Errorf("forged token: status = %d, want 401", got)
	}
}

func TestLogoutBlacklistsToken(t *testing.T) {
	m := newManager(t)
	_, token := doLogin(t, m, testUser, testPassword)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	if err := m.Logout(e.NewContext(req, rec)); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", rec.Code)
	}
	if got := callProtected(t, m, "Bearer "+token); got != http.StatusUnauthorized {
		t.Errorf("blacklisted token still accepted: status = %d, want 401", got)
	}
}

func TestChangePassword(t *testing.T) {
	m := newManager(t)
	e := echo.New()
	change := func(current, next string) int {
		body := `{"current_password":"` + current + `","new_password":"` + next + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/password", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set(principalKey, Principal{Username: testUser, Role: store.RoleAdmin})
		if err := m.ChangePassword(c); err != nil {
			t.Fatalf("ChangePassword: %v", err)
		}
		return rec.Code
	}

	if got := change("wrong-current", "new-password-123"); got != http.StatusUnauthorized {
		t.Errorf("wrong current password: status = %d, want 401", got)
	}
	if got := change(testPassword, "short"); got != http.StatusBadRequest {
		t.Errorf("too-short new password: status = %d, want 400", got)
	}
	if got := change(testPassword, "new-password-123"); got != http.StatusOK {
		t.Errorf("valid change: status = %d, want 200", got)
	}
	if rec, _ := doLogin(t, m, testUser, testPassword); rec.Code != http.StatusUnauthorized {
		t.Errorf("old password still accepted after change: status = %d", rec.Code)
	}
	if rec, _ := doLogin(t, m, testUser, "new-password-123"); rec.Code != http.StatusOK {
		t.Errorf("new password rejected after change: status = %d", rec.Code)
	}
}

// TestLegacyPasswordMigration: on first boot with an empty users table, the
// bcrypt hash persisted by the pre-RBAC code must win over the env password.
func TestLegacyPasswordMigration(t *testing.T) {
	dir := t.TempDir()
	hash, err := bcrypt.GenerateFromPassword([]byte("runtime-changed-pass"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "auth"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth", "password.bcrypt"), hash, 0600); err != nil {
		t.Fatal(err)
	}

	m := newManagerIn(t, dir) // initialPassword is testPassword, must be ignored
	if rec, _ := doLogin(t, m, testUser, "runtime-changed-pass"); rec.Code != http.StatusOK {
		t.Errorf("legacy file password rejected: status = %d", rec.Code)
	}
	if rec, _ := doLogin(t, m, testUser, testPassword); rec.Code != http.StatusUnauthorized {
		t.Errorf("env password accepted despite legacy file: status = %d", rec.Code)
	}
}

func TestLoginIncludesRoleClaim(t *testing.T) {
	m := newManager(t)
	_, token := doLogin(t, m, testUser, testPassword)
	p, err := m.validate(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if p.Username != testUser || p.Role != store.RoleAdmin {
		t.Errorf("principal = %+v, want admin/%s", p, testUser)
	}
}

func TestAuthorize(t *testing.T) {
	cases := []struct {
		role   string
		action Action
		want   bool
	}{
		{store.RoleAdmin, ActionPull, true},
		{store.RoleAdmin, ActionPush, true},
		{store.RoleAdmin, ActionDelete, true},
		{store.RoleAdmin, ActionAdmin, true},
		{store.RolePusher, ActionPull, true},
		{store.RolePusher, ActionPush, true},
		{store.RolePusher, ActionDelete, false},
		{store.RolePusher, ActionAdmin, false},
		{store.RoleReader, ActionPull, true},
		{store.RoleReader, ActionPush, false},
		{store.RoleReader, ActionDelete, false},
		{store.RoleReader, ActionAdmin, false},
	}
	for _, tc := range cases {
		p := Principal{Username: "u", Role: tc.role}
		if got := Authorize(p, tc.action, "any/repo"); got != tc.want {
			t.Errorf("Authorize(%s, %s) = %v, want %v", tc.role, tc.action, got, tc.want)
		}
	}
}

func TestAuthorizeRepoPatterns(t *testing.T) {
	p := Principal{Username: "u", Role: store.RolePusher, RepoPatterns: []string{"team-a/*", "shared"}}
	for repo, want := range map[string]bool{
		"team-a/app":        true,
		"team-a/sub/deeper": true, // '*' crosses slashes
		"shared":            true,
		"team-b/app":        false,
		"team-a":            false,
	} {
		if got := Authorize(p, ActionPush, repo); got != want {
			t.Errorf("Authorize(push, %q) = %v, want %v", repo, got, want)
		}
	}
	// No patterns → unrestricted.
	if !Authorize(Principal{Role: store.RolePusher}, ActionPush, "anything/at/all") {
		t.Error("empty patterns should not restrict")
	}
}

func TestRequireAdmin(t *testing.T) {
	e := echo.New()
	call := func(p *Principal) int {
		req := httptest.NewRequest(http.MethodDelete, "/api/admin/repositories", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		if p != nil {
			c.Set(principalKey, *p)
		}
		next := func(c echo.Context) error { return c.NoContent(http.StatusOK) }
		if err := RequireAdmin(next)(c); err != nil {
			t.Fatalf("RequireAdmin: %v", err)
		}
		return rec.Code
	}
	if got := call(&Principal{Username: "root", Role: store.RoleAdmin}); got != http.StatusOK {
		t.Errorf("admin: status = %d, want 200", got)
	}
	if got := call(&Principal{Username: "bob", Role: store.RoleReader}); got != http.StatusForbidden {
		t.Errorf("reader: status = %d, want 403", got)
	}
	if got := call(&Principal{Username: "carl", Role: store.RolePusher}); got != http.StatusForbidden {
		t.Errorf("pusher: status = %d, want 403", got)
	}
	if got := call(nil); got != http.StatusForbidden {
		t.Errorf("no principal: status = %d, want 403", got)
	}
}

func TestRefreshRotatesToken(t *testing.T) {
	m := newManager(t)
	rec, login := doLoginFull(t, m, testUser, testPassword)
	if rec.Code != http.StatusOK || login.RefreshToken == "" {
		t.Fatalf("login = %d, refresh_token empty=%v", rec.Code, login.RefreshToken == "")
	}

	rec, refreshed := doRefresh(t, m, login.RefreshToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if refreshed.Token == "" || refreshed.RefreshToken == "" {
		t.Fatal("refresh response missing tokens")
	}
	if refreshed.RefreshToken == login.RefreshToken {
		t.Error("refresh token was not rotated")
	}
	if got := callProtected(t, m, "Bearer "+refreshed.Token); got != http.StatusOK {
		t.Errorf("refreshed access token rejected: %d", got)
	}
	// Single use: the original refresh token must now be dead.
	if rec, _ := doRefresh(t, m, login.RefreshToken); rec.Code != http.StatusUnauthorized {
		t.Errorf("spent refresh token still accepted: %d", rec.Code)
	}
	if rec, _ := doRefresh(t, m, "completely-bogus"); rec.Code != http.StatusUnauthorized {
		t.Errorf("bogus refresh token accepted: %d", rec.Code)
	}
}

// TestLogoutSurvivesRestart: the revocation must come from SQLite, not memory.
func TestLogoutSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "dockyard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	m1, err := New(testUser, testPassword, testSecret, "", dir, st)
	if err != nil {
		t.Fatal(err)
	}
	_, token := doLogin(t, m1, testUser, testPassword)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	if err := m1.Logout(e.NewContext(req, rec)); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("logout = %d, %v", rec.Code, err)
	}

	// "Restart": a brand-new manager on the same store.
	m2, err := New(testUser, testPassword, testSecret, "", dir, st)
	if err != nil {
		t.Fatal(err)
	}
	if got := callProtected(t, m2, "Bearer "+token); got != http.StatusUnauthorized {
		t.Errorf("revoked token accepted after restart: %d", got)
	}
}

func TestSecretRotationGraceWindow(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "dockyard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	old, err := New(testUser, testPassword, "old-secret", "", dir, st)
	if err != nil {
		t.Fatal(err)
	}
	_, oldToken := doLogin(t, old, testUser, testPassword)

	// Rotated deployment: new secret, previous one in the grace window.
	rotated, err := New(testUser, testPassword, "new-secret", "old-secret", dir, st)
	if err != nil {
		t.Fatal(err)
	}
	if got := callProtected(t, rotated, "Bearer "+oldToken); got != http.StatusOK {
		t.Errorf("old-secret token rejected during grace window: %d", got)
	}

	// Grace window over: previous secret dropped.
	final, err := New(testUser, testPassword, "new-secret", "", dir, st)
	if err != nil {
		t.Fatal(err)
	}
	if got := callProtected(t, final, "Bearer "+oldToken); got != http.StatusUnauthorized {
		t.Errorf("old-secret token accepted after grace window: %d", got)
	}
	_, newToken := doLogin(t, rotated, testUser, testPassword)
	if got := callProtected(t, final, "Bearer "+newToken); got != http.StatusOK {
		t.Errorf("new-secret token rejected: %d", got)
	}
}

func TestSessionListAndRevoke(t *testing.T) {
	m := newManager(t)
	_, s1 := doLoginFull(t, m, testUser, testPassword)
	_, s2 := doLoginFull(t, m, testUser, testPassword)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/sessions", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(principalKey, Principal{Username: testUser, Role: store.RoleAdmin})
	if err := m.ListSessions(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("ListSessions = %d, %v", rec.Code, err)
	}
	var list struct {
		Sessions []struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"sessions"`
		Count int `json:"count"`
	}
	decodeJSON(t, rec, &list)
	if list.Count != 2 {
		t.Fatalf("session count = %d, want 2", list.Count)
	}

	// Revoke the first session; its refresh token must stop working while the
	// second session keeps refreshing fine.
	req = httptest.NewRequest(http.MethodDelete, "/api/admin/sessions/1", nil)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(strconv.FormatInt(list.Sessions[len(list.Sessions)-1].ID, 10))
	if err := m.RevokeSession(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("RevokeSession = %d, %v", rec.Code, err)
	}

	// One of the two refresh tokens is now dead; count the survivors.
	alive := 0
	for _, s := range []authResp{s1, s2} {
		if rec, _ := doRefresh(t, m, s.RefreshToken); rec.Code == http.StatusOK {
			alive++
		}
	}
	if alive != 1 {
		t.Errorf("alive refresh tokens after revoke = %d, want 1", alive)
	}
}
