package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"dockyard/internal/auth"

	"github.com/golang-jwt/jwt/v5"
)

// fakeIdP is a minimal OpenID provider: discovery, JWKS, and a token
// endpoint that checks PKCE and signs an ID token for whatever identity the
// test "authorized".
type fakeIdP struct {
	t      *testing.T
	server *httptest.Server
	key    *rsa.PrivateKey

	mu      sync.Mutex
	pending map[string]idpGrant // code → grant
}

type idpGrant struct {
	nonce, challenge string
	claims           map[string]any
}

func newFakeIdP(t *testing.T) *fakeIdP {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIdP{t: t, key: key, pending: map[string]idpGrant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		base := idp.server.URL
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                base,
			"authorization_endpoint":                base + "/authorize",
			"token_endpoint":                        base + "/token",
			"jwks_uri":                              base + "/keys",
			"userinfo_endpoint":                     base + "/userinfo",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		enc := base64.RawURLEncoding
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "alg": "RS256", "use": "sig",
			"n": enc.EncodeToString(key.N.Bytes()),
			"e": enc.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		idp.mu.Lock()
		grant, ok := idp.pending[r.Form.Get("code")]
		delete(idp.pending, r.Form.Get("code"))
		idp.mu.Unlock()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != grant.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		claims := jwt.MapClaims{
			"iss": idp.server.URL, "aud": "dockyard", "nonce": grant.nonce,
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
		}
		for k, v := range grant.claims {
			claims[k] = v
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tok.Header["kid"] = "k1"
		signed, err := tok.SignedString(key)
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "opaque", "token_type": "Bearer", "expires_in": 3600, "id_token": signed,
		})
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

// authorize plays the user approving the sign-in at the IdP: it reads the
// authorization request Dockyard redirected to and returns the callback URL
// the IdP would send the browser to.
func (idp *fakeIdP) authorize(authURL string, claims map[string]any) string {
	u, err := url.Parse(authURL)
	if err != nil {
		idp.t.Fatal(err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("nonce") == "" || q.Get("state") == "" {
		idp.t.Fatalf("authorization request lacks PKCE/nonce/state: %s", authURL)
	}
	code := "code-" + q.Get("state")
	idp.mu.Lock()
	idp.pending[code] = idpGrant{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), claims: claims}
	idp.mu.Unlock()
	return q.Get("redirect_uri") + "?code=" + code + "&state=" + url.QueryEscape(q.Get("state"))
}

var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// ssoLogin runs the browser side of a sign-in and returns where the
// callback sent the browser (the /login#… fragment).
func ssoLogin(t *testing.T, base string, idp *fakeIdP, claims map[string]any) string {
	t.Helper()
	resp, err := noRedirect.Get(base + "/api/admin/auth/oidc/login?return=/images")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login = %d", resp.StatusCode)
	}
	callback := idp.authorize(resp.Header.Get("Location"), claims)
	req, _ := http.NewRequest(http.MethodGet, callback, nil)
	for _, c := range resp.Cookies() {
		req.AddCookie(c)
	}
	back, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = back.Body.Close()
	return back.Header.Get("Location")
}

func fragmentValue(location, key string) string {
	_, fragment, _ := strings.Cut(location, "#")
	values, _ := url.ParseQuery(fragment)
	return values.Get(key)
}

func exchange(t *testing.T, base, code string) (int, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"code": code})
	status, raw := apiCall(t, base, http.MethodPost, "/api/admin/auth/oidc/exchange", "", body)
	var resp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(raw, &resp)
	return status, resp.Token
}

type meResponse struct {
	Username     string `json:"username"`
	Role         string `json:"role"`
	AuthSource   string `json:"auth_source"`
	HasCLISecret bool   `json:"has_cli_secret"`
}

func me(t *testing.T, base, token string) meResponse {
	t.Helper()
	status, raw := apiCall(t, base, http.MethodGet, "/api/admin/auth/me", token, nil)
	if status != http.StatusOK {
		t.Fatalf("me = %d: %s", status, raw)
	}
	var m meResponse
	_ = json.Unmarshal(raw, &m)
	return m
}

func TestOIDCSignIn(t *testing.T) {
	idp := newFakeIdP(t)
	srv := httptest.NewServer(newTestServer(t, func(s *Server) {
		s.v2AuthEnabled = true
		s.auth.EnableOIDC(auth.OIDCConfig{
			IssuerURL:    idp.server.URL,
			ClientID:     "dockyard",
			ClientSecret: "s3cret",
			GroupsClaim:  "groups",
			AdminGroups:  []string{"platform"},
			PusherGroups: []string{"developers"},
			DefaultRole:  "none",
			DisplayName:  "Company SSO",
		})
	}))
	t.Cleanup(srv.Close)
	adminJWT := loginAs(t, srv.URL, "admin", "test-password")

	alice := map[string]any{"sub": "u-1", "preferred_username": "alice", "groups": []string{"developers"}}
	var aliceToken string

	t.Run("advertises the provider for the login page", func(t *testing.T) {
		_, raw := apiCall(t, srv.URL, http.MethodGet, "/api/admin/auth/oidc", "", nil)
		if !strings.Contains(string(raw), `"enabled":true`) || !strings.Contains(string(raw), "Company SSO") {
			t.Fatalf("oidc info = %s", raw)
		}
	})

	t.Run("first sign-in creates the account with the role of its groups", func(t *testing.T) {
		location := ssoLogin(t, srv.URL, idp, alice)
		code := fragmentValue(location, "oidc_code")
		if code == "" || fragmentValue(location, "return") != "/images" {
			t.Fatalf("callback redirected to %q", location)
		}
		var status int
		status, aliceToken = exchange(t, srv.URL, code)
		if status != http.StatusOK || aliceToken == "" {
			t.Fatalf("exchange = %d", status)
		}
		if got := me(t, srv.URL, aliceToken); got.Username != "alice" || got.Role != "pusher" || got.AuthSource != "oidc" {
			t.Fatalf("me = %+v", got)
		}
		if status, _ := exchange(t, srv.URL, code); status != http.StatusUnauthorized {
			t.Fatalf("reusing the one-time code = %d, want 401", status)
		}
	})

	t.Run("the role follows the groups at every sign-in", func(t *testing.T) {
		promoted := map[string]any{"sub": "u-1", "preferred_username": "alice-renamed", "groups": []string{"platform"}}
		_, token := exchange(t, srv.URL, fragmentValue(ssoLogin(t, srv.URL, idp, promoted), "oidc_code"))
		got := me(t, srv.URL, token)
		if got.Role != "admin" || got.Username != "alice" {
			t.Fatalf("after promotion: %+v (the username must stay stable)", got)
		}
	})

	t.Run("users in no mapped group are refused when the default is none", func(t *testing.T) {
		outsider := map[string]any{"sub": "u-2", "preferred_username": "mallory", "groups": []string{"sales"}}
		if reason := fragmentValue(ssoLogin(t, srv.URL, idp, outsider), "oidc_error"); reason != "not_allowed" {
			t.Fatalf("outsider sign-in error = %q, want not_allowed", reason)
		}
	})

	t.Run("an SSO identity cannot take over a local account with the same name", func(t *testing.T) {
		createUser(t, srv.URL, adminJWT, "carol", "carol-password", "reader")
		impostor := map[string]any{"sub": "u-3", "preferred_username": "carol", "groups": []string{"platform"}}
		if reason := fragmentValue(ssoLogin(t, srv.URL, idp, impostor), "oidc_error"); reason != "username_taken" {
			t.Fatalf("collision error = %q, want username_taken", reason)
		}
		carol := loginAs(t, srv.URL, "carol", "carol-password")
		if got := me(t, srv.URL, carol); got.Role != "reader" || got.AuthSource != "local" {
			t.Fatalf("local carol changed: %+v", got)
		}
	})

	t.Run("a forged state is refused", func(t *testing.T) {
		resp, err := noRedirect.Get(srv.URL + "/api/admin/auth/oidc/login")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		callback := idp.authorize(resp.Header.Get("Location"), alice)
		forged := strings.Replace(callback, "state=", "state=x", 1)
		req, _ := http.NewRequest(http.MethodGet, forged, nil)
		for _, c := range resp.Cookies() {
			req.AddCookie(c)
		}
		back, err := noRedirect.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = back.Body.Close()
		if reason := fragmentValue(back.Header.Get("Location"), "oidc_error"); reason != "expired" {
			t.Fatalf("forged state error = %q", reason)
		}
	})

	t.Run("the return path cannot leave the site", func(t *testing.T) {
		resp, err := noRedirect.Get(srv.URL + "/api/admin/auth/oidc/login?return=//evil.example/x")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		callback := idp.authorize(resp.Header.Get("Location"), alice)
		req, _ := http.NewRequest(http.MethodGet, callback, nil)
		for _, c := range resp.Cookies() {
			req.AddCookie(c)
		}
		back, _ := noRedirect.Do(req)
		_ = back.Body.Close()
		if ret := fragmentValue(back.Header.Get("Location"), "return"); ret != "/" {
			t.Fatalf("return = %q, want /", ret)
		}
	})

	t.Run("an SSO account uses a CLI secret for docker login", func(t *testing.T) {
		status, raw := apiCall(t, srv.URL, http.MethodPost, "/api/admin/auth/cli-secret", aliceToken, nil)
		if status != http.StatusCreated {
			t.Fatalf("create CLI secret = %d: %s", status, raw)
		}
		var created struct {
			Secret string `json:"secret"`
		}
		_ = json.Unmarshal(raw, &created)
		if !strings.HasPrefix(created.Secret, "dkc_") {
			t.Fatalf("unexpected secret %q", created.Secret)
		}
		if token := fetchToken(t, srv.URL, "alice", created.Secret); token == "" {
			t.Fatal("no registry token for the CLI secret")
		}
		if !me(t, srv.URL, aliceToken).HasCLISecret {
			t.Fatal("has_cli_secret not reported")
		}
		// No password works for an SSO account, not even an empty one.
		body, _ := json.Marshal(map[string]string{"username": "alice", "password": ""})
		if status, _ := apiCall(t, srv.URL, http.MethodPost, "/api/admin/auth/login", "", body); status != http.StatusUnauthorized {
			t.Fatalf("password login of an SSO account = %d, want 401", status)
		}
		if status, _ := apiCall(t, srv.URL, http.MethodDelete, "/api/admin/auth/cli-secret", aliceToken, nil); status != http.StatusOK {
			t.Fatalf("revoke CLI secret = %d", status)
		}
		if got := basicStatus(t, srv.URL, http.MethodGet, "/v2/", "alice", created.Secret); got != http.StatusUnauthorized {
			t.Fatalf("revoked CLI secret = %d, want 401", got)
		}
	})
}

func TestOIDCDisabled(t *testing.T) {
	srv := httptest.NewServer(newTestServer(t, nil))
	t.Cleanup(srv.Close)
	_, raw := apiCall(t, srv.URL, http.MethodGet, "/api/admin/auth/oidc", "", nil)
	if !strings.Contains(string(raw), `"enabled":false`) {
		t.Fatalf("oidc info without config = %s", raw)
	}
	if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/auth/oidc/login", "", nil); status != http.StatusNotFound {
		t.Fatalf("login without config = %d, want 404", status)
	}
}
