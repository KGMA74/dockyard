package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
)

func TestScannerTokenPullsOneRepositoryOnly(t *testing.T) {
	m := newManager(t)
	token, err := m.IssueScannerToken("team/app", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	p, err := m.validate(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !p.Scanner || !p.Registry || p.Username != ScannerUsername || p.consoleAllowed() {
		t.Fatalf("principal = %+v, want the registry-only scanner", p)
	}

	digest := "sha256:" + strings.Repeat("a", 64)
	cases := []struct {
		name, method, path string
		want               int
	}{
		{"pull its manifest", http.MethodGet, "/v2/team/app/manifests/" + digest, http.StatusOK},
		{"pull its blobs", http.MethodGet, "/v2/team/app/blobs/" + digest, http.StatusOK},
		{"HEAD its manifest", http.MethodHead, "/v2/team/app/manifests/1.0", http.StatusOK},
		{"ping", http.MethodGet, "/v2/", http.StatusOK},
		{"pull another repository", http.MethodGet, "/v2/team/other/manifests/1.0", http.StatusForbidden},
		{"pull a sibling with the same prefix", http.MethodGet, "/v2/team/app-evil/manifests/1.0", http.StatusForbidden},
		{"pull a nested repository", http.MethodGet, "/v2/team/app/sub/manifests/1.0", http.StatusForbidden},
		{"push a manifest", http.MethodPut, "/v2/team/app/manifests/1.0", http.StatusForbidden},
		{"start a blob upload", http.MethodPost, "/v2/team/app/blobs/uploads/", http.StatusForbidden},
		{"delete", http.MethodDelete, "/v2/team/app/manifests/" + digest, http.StatusForbidden},
		{"unknown endpoint", http.MethodGet, "/v2/team/app/referrers/x", http.StatusForbidden},
	}
	for _, tc := range cases {
		h, _ := v2Backend(m, false)
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d (%s)", tc.name, rec.Code, tc.want, rec.Body.String())
		}
	}

	// Not a console credential.
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/repositories", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	_ = m.Middleware()(func(c echo.Context) error { return c.NoContent(http.StatusOK) })(e.NewContext(req, rec))
	if rec.Code != http.StatusForbidden {
		t.Errorf("admin API with a scanner token = %d, want 403", rec.Code)
	}
}

func TestIssueScannerTokenRefusesPatternsAndBadNames(t *testing.T) {
	m := newManager(t)
	for _, repo := range []string{"", "*", "team/*", "team/../other", "/team/app", "team//app"} {
		if _, err := m.IssueScannerToken(repo, time.Minute); err == nil {
			t.Errorf("IssueScannerToken(%q) succeeded, want refused", repo)
		}
	}
	if _, err := m.IssueScannerToken("team/app", 0); err == nil {
		t.Error("IssueScannerToken with no lifetime succeeded, want refused")
	}
}

// Scanner tokens are only what IssueScannerToken signs. Anything else
// claiming to be the scanner — even signed with the right key — is refused,
// and the scanner has no password to trade at /v2/token.
func TestScannerTokenShapeIsEnforced(t *testing.T) {
	m := newManager(t)
	sign := func(claims jwt.MapClaims) string {
		if _, set := claims["exp"]; !set {
			claims["exp"] = time.Now().Add(time.Minute).Unix()
		}
		raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secrets[0])
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	base := func() jwt.MapClaims {
		return jwt.MapClaims{"aud": audRegistry, "sub": ScannerUsername, "scn": true, "repos": []string{"team/app"}}
	}

	forged := map[string]jwt.MapClaims{}
	forged["no repository"] = base()
	delete(forged["no repository"], "repos")
	forged["every repository"] = base()
	forged["every repository"]["repos"] = []string{"*"}
	forged["a pattern"] = base()
	forged["a pattern"]["repos"] = []string{"team/*"}
	forged["two repositories"] = base()
	forged["two repositories"]["repos"] = []string{"team/app", "team/other"}
	forged["another subject"] = base()
	forged["another subject"]["sub"] = "admin"
	forged["no registry audience"] = base()
	delete(forged["no registry audience"], "aud")
	forged["expired"] = base()
	forged["expired"]["exp"] = time.Now().Add(-time.Minute).Unix()
	for name, claims := range forged {
		if p, err := m.validate(sign(claims)); err == nil {
			t.Errorf("%s: accepted as %+v, want refused", name, p)
		}
	}

	if p, err := m.validate(sign(base())); err != nil || len(p.RepoPatterns) != 1 || p.RepoPatterns[0] != "team/app" {
		t.Fatalf("well-formed scanner token: %+v, %v", p, err)
	}

	if rec, _ := requestV2Token(t, m, false, ScannerUsername, "anything"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("/v2/token with the scanner's name = %d, want 401", rec.Code)
	}
	if rec, _ := requestV2Token(t, m, true, ScannerUsername, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("/v2/token with the scanner's name and an empty password = %d, want 401", rec.Code)
	}
}
