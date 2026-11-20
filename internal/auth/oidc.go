package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"dockyard/internal/store"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"golang.org/x/oauth2"
)

// Single sign-on with any OpenID Connect provider (Authentik, Keycloak,
// Authelia, Zitadel, Entra ID, Google, Dex…): authorization code flow with
// PKCE, state and nonce. The provider proves who the user is; Dockyard then
// issues its own session exactly as for a password login.
//
//  1. GET  /api/admin/auth/oidc/login     → redirect to the provider
//  2. GET  /api/admin/auth/oidc/callback  → verify, provision, redirect to
//     /login#oidc_code=<one-time code>
//  3. POST /api/admin/auth/oidc/exchange  → the SPA trades the code for its
//     tokens. Tokens never travel in a URL, so they never land in a proxy
//     log or the browser history.

const (
	oidcCookie     = "dockyard_oidc"
	oidcCookiePath = "/api/admin/auth/oidc"
	oidcFlowTTL    = 10 * time.Minute
	oidcCodeTTL    = time.Minute
	oidcCallback   = "/api/admin/auth/oidc/callback"
)

// OIDCConfig is the operator's provider setup (see config.Config).
type OIDCConfig struct {
	IssuerURL     string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	Scopes        []string
	UsernameClaim string
	GroupsClaim   string
	AdminGroups   []string
	PusherGroups  []string
	ReaderGroups  []string
	DefaultRole   string // reader | pusher | admin | none
	DisplayName   string
}

// RolesFromGroups reports whether the provider's groups decide roles.
func (c OIDCConfig) RolesFromGroups() bool {
	return len(c.AdminGroups)+len(c.PusherGroups)+len(c.ReaderGroups) > 0
}

// roleFor maps a user's groups to a role; "" means the user is not allowed.
func (c OIDCConfig) roleFor(groups []string) string {
	in := func(wanted []string) bool {
		return slices.ContainsFunc(wanted, func(g string) bool { return slices.Contains(groups, g) })
	}
	switch {
	case in(c.AdminGroups):
		return store.RoleAdmin
	case in(c.PusherGroups):
		return store.RolePusher
	case in(c.ReaderGroups):
		return store.RoleReader
	case store.ValidRole(c.DefaultRole):
		return c.DefaultRole
	default:
		return ""
	}
}

type oidcState struct {
	cfg OIDCConfig

	mu       sync.Mutex
	provider *oidc.Provider // discovered on first use: the IdP may start after us
	codes    map[string]pendingLogin
}

type pendingLogin struct {
	body    map[string]any
	expires time.Time
}

// EnableOIDC turns single sign-on on.
func (m *Manager) EnableOIDC(cfg OIDCConfig) {
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}
	if !slices.Contains(cfg.Scopes, oidc.ScopeOpenID) {
		cfg.Scopes = append([]string{oidc.ScopeOpenID}, cfg.Scopes...)
	}
	if cfg.UsernameClaim == "" {
		cfg.UsernameClaim = "preferred_username"
	}
	if cfg.DisplayName == "" {
		cfg.DisplayName = "SSO"
	}
	m.oidc = &oidcState{cfg: cfg, codes: map[string]pendingLogin{}}
}

func (s *oidcState) discover(ctx context.Context) (*oidc.Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.provider != nil {
		return s.provider, nil
	}
	p, err := oidc.NewProvider(ctx, s.cfg.IssuerURL)
	if err != nil {
		return nil, err
	}
	s.provider = p
	return p, nil
}

func (s *oidcState) oauth2Config(p *oidc.Provider, r *http.Request) *oauth2.Config {
	redirect := s.cfg.RedirectURL
	if redirect == "" {
		redirect = requestScheme(r) + "://" + r.Host + oidcCallback
	}
	return &oauth2.Config{
		ClientID:     s.cfg.ClientID,
		ClientSecret: s.cfg.ClientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  redirect,
		Scopes:       s.cfg.Scopes,
	}
}

func (s *oidcState) storeCode(body map[string]any) string {
	code := randomHex(32)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.codes {
		if now.After(v.expires) {
			delete(s.codes, k)
		}
	}
	s.codes[code] = pendingLogin{body: body, expires: now.Add(oidcCodeTTL)}
	return code
}

func (s *oidcState) takeCode(code string) (map[string]any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.codes[code]
	delete(s.codes, code) // single use, found or not
	if !ok || time.Now().After(p.expires) {
		return nil, false
	}
	return p.body, true
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// safeReturn keeps the post-login destination on this site: a path, never
// "//evil.example" or a full URL.
func safeReturn(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "/\\") {
		return "/"
	}
	return raw
}

// OIDCInfo — GET /api/admin/auth/oidc (public): whether to show the SSO button.
func (m *Manager) OIDCInfo(c echo.Context) error {
	if m.oidc == nil {
		return c.JSON(http.StatusOK, map[string]any{"enabled": false})
	}
	return c.JSON(http.StatusOK, map[string]any{"enabled": true, "name": m.oidc.cfg.DisplayName})
}

// OIDCLogin — GET /api/admin/auth/oidc/login?return=/path
func (m *Manager) OIDCLogin(c echo.Context) error {
	if m.oidc == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "single sign-on is not configured"})
	}
	provider, err := m.oidc.discover(c.Request().Context())
	if err != nil {
		slog.Error("oidc: provider discovery failed", "issuer", m.oidc.cfg.IssuerURL, "err", err)
		return c.Redirect(http.StatusFound, "/login#oidc_error=provider_unreachable")
	}
	state, nonce, verifier := randomHex(16), randomHex(16), oauth2.GenerateVerifier()
	flow, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"st":  state,
		"nc":  nonce,
		"pv":  verifier,
		"ret": safeReturn(c.QueryParam("return")),
		"exp": time.Now().Add(oidcFlowTTL).Unix(),
	}).SignedString(m.secrets[0])
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "cannot start sign-in"})
	}
	c.SetCookie(&http.Cookie{
		Name:     oidcCookie,
		Value:    flow,
		Path:     oidcCookiePath,
		MaxAge:   int(oidcFlowTTL.Seconds()),
		HttpOnly: true,
		Secure:   requestScheme(c.Request()) == "https",
		// Lax: the callback is a top-level navigation back from the IdP.
		SameSite: http.SameSiteLaxMode,
	})
	authURL := m.oidc.oauth2Config(provider, c.Request()).AuthCodeURL(state,
		oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
	return c.Redirect(http.StatusFound, authURL)
}

// oidcFailure sends the browser back to the login page with a reason the UI
// translates; details go to the log, not the URL.
func oidcFailure(c echo.Context, reason string, err error) error {
	slog.Warn("oidc: sign-in refused", "reason", reason, "err", err)
	return c.Redirect(http.StatusFound, "/login#oidc_error="+url.QueryEscape(reason))
}

// OIDCCallback — GET /api/admin/auth/oidc/callback?code=&state=
func (m *Manager) OIDCCallback(c echo.Context) error {
	if m.oidc == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "single sign-on is not configured"})
	}
	// The flow cookie is single use, whatever happens next.
	c.SetCookie(&http.Cookie{Name: oidcCookie, Value: "", Path: oidcCookiePath, MaxAge: -1, HttpOnly: true})

	if e := c.QueryParam("error"); e != "" {
		return oidcFailure(c, "provider_denied", errors.New(e+": "+c.QueryParam("error_description")))
	}
	cookie, err := c.Cookie(oidcCookie)
	if err != nil {
		return oidcFailure(c, "expired", err)
	}
	flowTok, err := m.parseToken(cookie.Value)
	if err != nil {
		return oidcFailure(c, "expired", err)
	}
	flow, _ := flowTok.Claims.(jwt.MapClaims)
	state, _ := flow["st"].(string)
	nonce, _ := flow["nc"].(string)
	verifier, _ := flow["pv"].(string)
	ret, _ := flow["ret"].(string)
	if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(c.QueryParam("state"))) != 1 {
		return oidcFailure(c, "expired", errors.New("state mismatch"))
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 30*time.Second)
	defer cancel()
	provider, err := m.oidc.discover(ctx)
	if err != nil {
		return oidcFailure(c, "provider_unreachable", err)
	}
	oauthCfg := m.oidc.oauth2Config(provider, c.Request())
	token, err := oauthCfg.Exchange(ctx, c.QueryParam("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		return oidcFailure(c, "exchange_failed", err)
	}
	rawID, _ := token.Extra("id_token").(string)
	idToken, err := provider.Verifier(&oidc.Config{ClientID: m.oidc.cfg.ClientID}).Verify(ctx, rawID)
	if err != nil {
		return oidcFailure(c, "invalid_token", err)
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		return oidcFailure(c, "invalid_token", errors.New("nonce mismatch"))
	}

	claims := map[string]any{}
	if err := idToken.Claims(&claims); err != nil {
		return oidcFailure(c, "invalid_token", err)
	}
	// Many providers only put groups (or the username) in UserInfo.
	if claims[m.oidc.cfg.UsernameClaim] == nil || (m.oidc.cfg.RolesFromGroups() && claims[m.oidc.cfg.GroupsClaim] == nil) {
		if info, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token)); err == nil {
			extra := map[string]any{}
			if info.Claims(&extra) == nil {
				for k, v := range extra {
					if _, set := claims[k]; !set {
						claims[k] = v
					}
				}
			}
		}
	}

	u, reason, err := m.provisionOIDCUser(idToken.Issuer+"|"+idToken.Subject, claims)
	if err != nil {
		m.auditRecord(stringClaim(claims, m.oidc.cfg.UsernameClaim), "login", c.RealIP(), "failure", "sso: "+reason)
		return oidcFailure(c, reason, err)
	}
	body, err := m.issueSession(c, u)
	if err != nil {
		return oidcFailure(c, "session_failed", err)
	}
	m.auditRecord(u.Username, "login", c.RealIP(), "success", "sso")
	code := m.oidc.storeCode(body)
	return c.Redirect(http.StatusFound, "/login#oidc_code="+code+"&return="+url.QueryEscape(safeReturn(ret)))
}

// provisionOIDCUser finds or creates the account of an SSO identity and
// syncs its role. reason is a stable key for the UI when it fails.
func (m *Manager) provisionOIDCUser(subject string, claims map[string]any) (*store.User, string, error) {
	cfg := m.oidc.cfg
	groups := stringsClaim(claims, cfg.GroupsClaim)
	role := cfg.roleFor(groups)
	if role == "" {
		return nil, "not_allowed", fmt.Errorf("no role for groups %v", groups)
	}

	u, err := m.users.UserByOIDCSubject(subject)
	switch {
	case err == nil:
		// Known identity: keep its username (the IdP may let people rename
		// themselves), sync the role when the IdP's groups decide it.
		if cfg.RolesFromGroups() && u.Role != role {
			if err := m.users.SetUserRole(u.Username, role); err != nil {
				slog.Warn("oidc: role sync refused", "user", u.Username, "role", role, "err", err)
			} else {
				u.Role = role
			}
		}
		return u, "", nil
	case !errors.Is(err, store.ErrUserNotFound):
		return nil, "session_failed", err
	}

	username := stringClaim(claims, cfg.UsernameClaim)
	if !validSSOUsername(username) {
		username = stringClaim(claims, "email")
	}
	if !validSSOUsername(username) {
		return nil, "username_invalid", fmt.Errorf("claim %q gives no usable username", cfg.UsernameClaim)
	}
	// An existing local account is never taken over by an SSO identity that
	// merely carries the same name.
	if _, err := m.users.GetUser(username); err == nil {
		return nil, "username_taken", fmt.Errorf("local account %q already exists", username)
	}
	u, err = m.users.CreateOIDCUser(username, role, subject)
	if err != nil {
		return nil, "session_failed", err
	}
	slog.Info("oidc: account created on first sign-in", "user", username, "role", role)
	return u, "", nil
}

func validSSOUsername(name string) bool {
	if name == "" || len(name) > 64 || strings.EqualFold(name, anonymousUser) || strings.EqualFold(name, ScannerUsername) {
		return false
	}
	for _, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '.' || r == '_' || r == '-' || r == '@'
		if !ok {
			return false
		}
	}
	return true
}

func stringClaim(claims map[string]any, name string) string {
	s, _ := claims[name].(string)
	return s
}

// stringsClaim reads a list claim; providers send it as an array or, with a
// single value, sometimes as a plain string.
func stringsClaim(claims map[string]any, name string) []string {
	switch v := claims[name].(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// OIDCExchange — POST /api/admin/auth/oidc/exchange {code}: the SPA trades
// the one-time code from the callback redirect for its session tokens.
func (m *Manager) OIDCExchange(c echo.Context) error {
	if m.oidc == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "single sign-on is not configured"})
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := c.Bind(&body); err != nil || body.Code == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "code required"})
	}
	resp, ok := m.oidc.takeCode(body.Code)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "sign-in code expired or already used"})
	}
	return c.JSON(http.StatusOK, resp)
}
