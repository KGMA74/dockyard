package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"dockyard/internal/store"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/labstack/echo/v4"
)

// Single sign-on can be configured two ways:
//
//   - by the deployment (OIDC_ISSUER_URL and the other OIDC_* variables, the
//     Helm chart's oidc values): the environment owns the configuration and
//     the console shows it read-only, so a GitOps setup is never overridden
//     behind its back;
//   - from the console (Settings → Single sign-on): saved in the database,
//     applied at once, no restart. OIDC_CLIENT_SECRET may still come from the
//     deployment (a Kubernetes Secret) while the rest is set here.

const (
	sealSSOClientSecret = "oidc-client-secret"

	ssoManagedByEnv     = "env"
	ssoManagedByConsole = "console"

	ssoTestTimeout = 10 * time.Second
)

// SetOIDCEnvClientSecret supplies OIDC_CLIENT_SECRET for a configuration made
// in the console. Ignored when the deployment configures SSO itself.
func (m *Manager) SetOIDCEnvClientSecret(secret string) {
	m.oidcMu.Lock()
	m.oidcEnvSecret = secret
	m.oidcMu.Unlock()
	if err := m.reloadOIDC(); err != nil {
		slog.Error("sso: saved configuration not applied", "err", err)
	}
}

// reloadOIDC applies the configuration saved from the console, unless the
// deployment owns it.
func (m *Manager) reloadOIDC() error {
	m.oidcMu.Lock()
	defer m.oidcMu.Unlock()
	if m.oidcEnvManaged {
		return nil
	}
	saved, err := m.users.GetOIDCSettings()
	if err != nil {
		m.oidc.Store(nil)
		return err
	}
	if !saved.Enabled {
		m.oidc.Store(nil)
		return nil
	}
	secret, err := m.ssoClientSecret(saved)
	if err != nil {
		// Without its secret a confidential client cannot sign anyone in:
		// keep SSO off and say why, rather than fail every sign-in.
		m.oidc.Store(nil)
		return fmt.Errorf("client secret: %w — enter it again in Settings", err)
	}
	m.oidc.Store(&oidcState{cfg: normalizeOIDC(OIDCConfig{
		IssuerURL:     saved.IssuerURL,
		ClientID:      saved.ClientID,
		ClientSecret:  secret,
		RedirectURL:   saved.RedirectURL,
		Scopes:        saved.Scopes,
		UsernameClaim: saved.UsernameClaim,
		GroupsClaim:   saved.GroupsClaim,
		AdminGroups:   saved.AdminGroups,
		PusherGroups:  saved.PusherGroups,
		ReaderGroups:  saved.ReaderGroups,
		DefaultRole:   saved.DefaultRole,
		DisplayName:   saved.DisplayName,
	})})
	return nil
}

// ssoClientSecret returns the client secret of a console configuration:
// OIDC_CLIENT_SECRET when the deployment sets it, the sealed one otherwise.
// A secret sealed with a previous JWT secret is sealed again with the
// current one. Called with oidcMu held.
func (m *Manager) ssoClientSecret(saved *store.OIDCSettings) (string, error) {
	if m.oidcEnvSecret != "" {
		return m.oidcEnvSecret, nil
	}
	secret, stale, err := m.unseal(sealSSOClientSecret, saved.ClientSecretEnc)
	if err != nil {
		return "", err
	}
	if stale {
		if resealed, err := m.seal(sealSSOClientSecret, secret); err == nil {
			saved.ClientSecretEnc = resealed
			if err := m.users.SaveOIDCSettings(saved); err != nil {
				slog.Warn("sso: could not re-seal the client secret with the current JWT secret", "err", err)
			}
		}
	}
	return secret, nil
}

// OIDCSummary is the SSO line of the configuration page: the issuer (or
// "off") and who manages it.
func (m *Manager) OIDCSummary() (issuer, managedBy string) {
	m.oidcMu.Lock()
	managedBy = ssoManagedByConsole
	if m.oidcEnvManaged {
		managedBy = ssoManagedByEnv
	}
	m.oidcMu.Unlock()
	if s := m.oidc.Load(); s != nil {
		return s.cfg.IssuerURL, managedBy
	}
	return "off", managedBy
}

type ssoSettingsView struct {
	ManagedBy           string   `json:"managed_by"`
	Enabled             bool     `json:"enabled"`
	IssuerURL           string   `json:"issuer_url"`
	ClientID            string   `json:"client_id"`
	ClientSecretSet     bool     `json:"client_secret_set"`
	ClientSecretFromEnv bool     `json:"client_secret_from_env"`
	RedirectURL         string   `json:"redirect_url"`
	CallbackURL         string   `json:"callback_url"`
	DisplayName         string   `json:"display_name"`
	Scopes              []string `json:"scopes"`
	UsernameClaim       string   `json:"username_claim"`
	GroupsClaim         string   `json:"groups_claim"`
	AdminGroups         []string `json:"admin_groups"`
	PusherGroups        []string `json:"pusher_groups"`
	ReaderGroups        []string `json:"reader_groups"`
	DefaultRole         string   `json:"default_role"`
	UpdatedBy           string   `json:"updated_by"`
	UpdatedAt           string   `json:"updated_at"`
	// Error explains why an enabled configuration is not active.
	Error string `json:"error,omitempty"`
}

func nonNilList(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// callbackURL is what to register at the provider as the redirect URI.
func callbackURL(r *http.Request, configured string) string {
	if configured != "" {
		return configured
	}
	return requestScheme(r) + "://" + r.Host + oidcCallback
}

func (m *Manager) ssoView(r *http.Request) (ssoSettingsView, error) {
	m.oidcMu.Lock()
	defer m.oidcMu.Unlock()
	if m.oidcEnvManaged {
		cfg := m.oidcEnvCfg
		return ssoSettingsView{
			ManagedBy:           ssoManagedByEnv,
			Enabled:             true,
			IssuerURL:           cfg.IssuerURL,
			ClientID:            cfg.ClientID,
			ClientSecretSet:     cfg.ClientSecret != "",
			ClientSecretFromEnv: cfg.ClientSecret != "",
			RedirectURL:         cfg.RedirectURL,
			CallbackURL:         callbackURL(r, cfg.RedirectURL),
			DisplayName:         cfg.DisplayName,
			Scopes:              nonNilList(cfg.Scopes),
			UsernameClaim:       cfg.UsernameClaim,
			GroupsClaim:         cfg.GroupsClaim,
			AdminGroups:         nonNilList(cfg.AdminGroups),
			PusherGroups:        nonNilList(cfg.PusherGroups),
			ReaderGroups:        nonNilList(cfg.ReaderGroups),
			DefaultRole:         cfg.DefaultRole,
		}, nil
	}
	saved, err := m.users.GetOIDCSettings()
	if err != nil {
		return ssoSettingsView{}, err
	}
	view := ssoSettingsView{
		ManagedBy:           ssoManagedByConsole,
		Enabled:             saved.Enabled,
		IssuerURL:           saved.IssuerURL,
		ClientID:            saved.ClientID,
		ClientSecretSet:     m.oidcEnvSecret != "" || saved.ClientSecretEnc != "",
		ClientSecretFromEnv: m.oidcEnvSecret != "",
		RedirectURL:         saved.RedirectURL,
		CallbackURL:         callbackURL(r, saved.RedirectURL),
		DisplayName:         saved.DisplayName,
		Scopes:              nonNilList(saved.Scopes),
		UsernameClaim:       saved.UsernameClaim,
		GroupsClaim:         saved.GroupsClaim,
		AdminGroups:         nonNilList(saved.AdminGroups),
		PusherGroups:        nonNilList(saved.PusherGroups),
		ReaderGroups:        nonNilList(saved.ReaderGroups),
		DefaultRole:         saved.DefaultRole,
		UpdatedBy:           saved.UpdatedBy,
		UpdatedAt:           saved.UpdatedAt,
	}
	if saved.Enabled && m.oidc.Load() == nil {
		view.Error = "the saved configuration could not be applied: enter the client secret again"
	}
	return view, nil
}

// SSOSettings — GET /api/admin/sso (admin)
func (m *Manager) SSOSettings(c echo.Context) error {
	view, err := m.ssoView(c.Request())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, view)
}

type ssoSettingsInput struct {
	Enabled     bool   `json:"enabled"`
	IssuerURL   string `json:"issuer_url"`
	ClientID    string `json:"client_id"`
	// ClientSecret: absent keeps the saved one, "" removes it.
	ClientSecret  *string  `json:"client_secret"`
	RedirectURL   string   `json:"redirect_url"`
	DisplayName   string   `json:"display_name"`
	Scopes        []string `json:"scopes"`
	UsernameClaim string   `json:"username_claim"`
	GroupsClaim   string   `json:"groups_claim"`
	AdminGroups   []string `json:"admin_groups"`
	PusherGroups  []string `json:"pusher_groups"`
	ReaderGroups  []string `json:"reader_groups"`
	DefaultRole   string   `json:"default_role"`
}

// providerURL checks an issuer or redirect URL: absolute http(s), a host, no
// credentials, query or fragment.
func providerURL(field, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%s must be an absolute http(s) URL", field)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%s must not contain credentials, a query or a fragment", field)
	}
	return nil
}

// cleanList trims, drops empty entries and duplicates.
func cleanList(field string, in []string, maxItems int) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || slices.Contains(out, v) {
			continue
		}
		if len(v) > 200 {
			return nil, fmt.Errorf("%s: %q is too long", field, v[:20]+"…")
		}
		out = append(out, v)
	}
	if len(out) > maxItems {
		return nil, fmt.Errorf("%s: at most %d entries", field, maxItems)
	}
	return out, nil
}

func validClaimName(name string) bool {
	if name == "" || len(name) > 100 {
		return false
	}
	for _, r := range name {
		if !claimRune(r) {
			return false
		}
	}
	return true
}

func claimRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_-.:/", r)
}

// normalizeInput validates a configuration from the console and fills its
// defaults.
func normalizeInput(in *ssoSettingsInput) error {
	in.IssuerURL = strings.TrimSpace(in.IssuerURL)
	in.ClientID = strings.TrimSpace(in.ClientID)
	in.RedirectURL = strings.TrimSpace(in.RedirectURL)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.UsernameClaim = strings.TrimSpace(in.UsernameClaim)
	in.GroupsClaim = strings.TrimSpace(in.GroupsClaim)

	if in.IssuerURL != "" {
		if err := providerURL("issuer_url", in.IssuerURL); err != nil {
			return err
		}
	}
	if in.RedirectURL != "" {
		if err := providerURL("redirect_url", in.RedirectURL); err != nil {
			return err
		}
	}
	if in.Enabled && (in.IssuerURL == "" || in.ClientID == "") {
		return errors.New("issuer_url and client_id are required to enable single sign-on")
	}
	if len(in.ClientID) > 255 {
		return errors.New("client_id is too long")
	}
	if in.DisplayName == "" {
		in.DisplayName = "SSO"
	}
	if len([]rune(in.DisplayName)) > 40 {
		return errors.New("display_name: at most 40 characters")
	}
	if in.UsernameClaim == "" {
		in.UsernameClaim = "preferred_username"
	}
	if in.GroupsClaim == "" {
		in.GroupsClaim = "groups"
	}
	if !validClaimName(in.UsernameClaim) || !validClaimName(in.GroupsClaim) {
		return errors.New("claim names may only contain letters, digits and _ - . : /")
	}
	if in.DefaultRole == "" {
		in.DefaultRole = store.RoleReader
	}
	if in.DefaultRole != "none" && !store.ValidRole(in.DefaultRole) {
		return errors.New("default_role must be reader, pusher, admin or none")
	}

	var err error
	if in.Scopes, err = cleanList("scopes", in.Scopes, 20); err != nil {
		return err
	}
	for _, s := range in.Scopes {
		if strings.ContainsAny(s, " \t\"") {
			return fmt.Errorf("scopes: %q is not a valid scope", s)
		}
	}
	if len(in.Scopes) == 0 {
		in.Scopes = []string{oidc.ScopeOpenID, "profile", "email", "groups"}
	} else if !slices.Contains(in.Scopes, oidc.ScopeOpenID) {
		in.Scopes = append([]string{oidc.ScopeOpenID}, in.Scopes...)
	}
	for _, list := range []struct {
		name string
		v    *[]string
	}{{"admin_groups", &in.AdminGroups}, {"pusher_groups", &in.PusherGroups}, {"reader_groups", &in.ReaderGroups}} {
		if *list.v, err = cleanList(list.name, *list.v, 100); err != nil {
			return err
		}
	}
	return nil
}

// UpdateSSOSettings — PUT /api/admin/sso (admin). Saves and applies at once.
func (m *Manager) UpdateSSOSettings(c echo.Context) error {
	p, _ := CurrentPrincipal(c)
	var in ssoSettingsInput
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	if err := normalizeInput(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	m.oidcMu.Lock()
	envManaged, envSecret := m.oidcEnvManaged, m.oidcEnvSecret != ""
	m.oidcMu.Unlock()
	if envManaged {
		return c.JSON(http.StatusConflict, map[string]string{
			"error": "single sign-on is configured by the deployment (OIDC_ISSUER_URL); change it there",
		})
	}
	if in.ClientSecret != nil && envSecret {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "the client secret comes from OIDC_CLIENT_SECRET; change it in the deployment",
		})
	}

	saved, err := m.users.GetOIDCSettings()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	next := &store.OIDCSettings{
		Enabled:         in.Enabled,
		IssuerURL:       in.IssuerURL,
		ClientID:        in.ClientID,
		ClientSecretEnc: saved.ClientSecretEnc,
		RedirectURL:     in.RedirectURL,
		DisplayName:     in.DisplayName,
		Scopes:          in.Scopes,
		UsernameClaim:   in.UsernameClaim,
		GroupsClaim:     in.GroupsClaim,
		AdminGroups:     in.AdminGroups,
		PusherGroups:    in.PusherGroups,
		ReaderGroups:    in.ReaderGroups,
		DefaultRole:     in.DefaultRole,
		UpdatedBy:       p.Username,
	}
	if in.ClientSecret != nil {
		if len(*in.ClientSecret) > 1024 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "client_secret is too long"})
		}
		if next.ClientSecretEnc, err = m.seal(sealSSOClientSecret, *in.ClientSecret); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "cannot protect the client secret"})
		}
	}
	if err := m.users.SaveOIDCSettings(next); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if err := m.reloadOIDC(); err != nil {
		slog.Error("sso: saved configuration not applied", "err", err)
	}
	state := "disabled"
	if in.Enabled {
		state = "enabled, issuer " + in.IssuerURL
	}
	// The admin API audit middleware already records the change itself.
	slog.Info("sso: configuration changed from the console", "by", p.Username, "state", state)
	return m.SSOSettings(c)
}

// TestSSOSettings — POST /api/admin/sso/test {issuer_url} (admin): runs
// OpenID discovery against an issuer, so a typo shows before anyone is sent
// to a broken sign-in page.
func (m *Manager) TestSSOSettings(c echo.Context) error {
	var in struct {
		IssuerURL string `json:"issuer_url"`
	}
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	issuer := strings.TrimSpace(in.IssuerURL)
	if issuer == "" {
		if s := m.oidc.Load(); s != nil {
			issuer = s.cfg.IssuerURL
		}
	}
	if issuer == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "issuer_url required"})
	}
	if err := providerURL("issuer_url", issuer); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), ssoTestTimeout)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		msg := err.Error()
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
		return c.JSON(http.StatusOK, map[string]any{"ok": false, "issuer": issuer, "error": msg})
	}
	var meta struct {
		UserInfo string   `json:"userinfo_endpoint"`
		Scopes   []string `json:"scopes_supported"`
		Claims   []string `json:"claims_supported"`
	}
	_ = provider.Claims(&meta)
	return c.JSON(http.StatusOK, map[string]any{
		"ok":                     true,
		"issuer":                 issuer,
		"authorization_endpoint": provider.Endpoint().AuthURL,
		"token_endpoint":         provider.Endpoint().TokenURL,
		"userinfo_endpoint":      meta.UserInfo,
		"scopes_supported":       nonNilList(meta.Scopes),
		"claims_supported":       nonNilList(meta.Claims),
	})
}
