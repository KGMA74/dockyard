package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dockyard/config"
	"dockyard/internal/auth"
)

type ssoView struct {
	ManagedBy           string   `json:"managed_by"`
	Enabled             bool     `json:"enabled"`
	IssuerURL           string   `json:"issuer_url"`
	ClientSecretSet     bool     `json:"client_secret_set"`
	ClientSecretFromEnv bool     `json:"client_secret_from_env"`
	CallbackURL         string   `json:"callback_url"`
	Scopes              []string `json:"scopes"`
	AdminGroups         []string `json:"admin_groups"`
	DefaultRole         string   `json:"default_role"`
	UpdatedBy           string   `json:"updated_by"`
}

func putSSO(t *testing.T, base, token string, body map[string]any) (int, ssoView, string) {
	t.Helper()
	raw, _ := json.Marshal(body)
	status, resp := apiCall(t, base, http.MethodPut, "/api/admin/sso", token, raw)
	var v ssoView
	_ = json.Unmarshal(resp, &v)
	return status, v, string(resp)
}

func getSSO(t *testing.T, base, token string) ssoView {
	t.Helper()
	status, raw := apiCall(t, base, http.MethodGet, "/api/admin/sso", token, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /sso = %d: %s", status, raw)
	}
	var v ssoView
	_ = json.Unmarshal(raw, &v)
	return v
}

func TestSSOConfiguredFromTheConsole(t *testing.T) {
	idp := newFakeIdP(t)
	var server *Server
	srv := httptest.NewServer(newTestServer(t, func(s *Server) {
		server = s
		s.configSummary = newConfigSummary(&config.Config{}, modeEmbedded, 0, false, "1h")
	}))
	t.Cleanup(srv.Close)
	adminJWT := loginAs(t, srv.URL, "admin", "test-password")

	config := map[string]any{
		"enabled":       true,
		"issuer_url":    idp.server.URL,
		"client_id":     "dockyard",
		"client_secret": "s3cret-value",
		"display_name":  "Company SSO",
		"admin_groups":  []string{"platform", " platform ", ""},
		"default_role":  "reader",
	}

	t.Run("starts off and managed from the console", func(t *testing.T) {
		v := getSSO(t, srv.URL, adminJWT)
		if v.ManagedBy != "console" || v.Enabled || v.ClientSecretSet {
			t.Fatalf("initial view %+v", v)
		}
		if !strings.HasSuffix(v.CallbackURL, "/api/admin/auth/oidc/callback") {
			t.Fatalf("callback hint %q", v.CallbackURL)
		}
	})

	t.Run("saving enables sign-in at once, without a restart", func(t *testing.T) {
		status, v, raw := putSSO(t, srv.URL, adminJWT, config)
		if status != http.StatusOK {
			t.Fatalf("PUT = %d: %s", status, raw)
		}
		if strings.Contains(raw, "s3cret-value") {
			t.Fatalf("the response echoes the client secret: %s", raw)
		}
		if !v.Enabled || !v.ClientSecretSet || v.UpdatedBy != "admin" || len(v.AdminGroups) != 1 || v.Scopes[0] != "openid" {
			t.Fatalf("saved view %+v", v)
		}
		_, info := apiCall(t, srv.URL, http.MethodGet, "/api/admin/auth/oidc", "", nil)
		if !strings.Contains(string(info), `"enabled":true`) || !strings.Contains(string(info), "Company SSO") {
			t.Fatalf("login page info = %s", info)
		}
		claims := map[string]any{"sub": "u-1", "preferred_username": "alice", "groups": []string{"platform"}}
		status, token := exchange(t, srv.URL, fragmentValue(ssoLogin(t, srv.URL, idp, claims), "oidc_code"))
		if status != http.StatusOK {
			t.Fatalf("SSO sign-in after saving = %d", status)
		}
		if got := me(t, srv.URL, token); got.Role != "admin" || got.AuthSource != "oidc" {
			t.Fatalf("me = %+v", got)
		}
	})

	t.Run("the secret is stored encrypted and kept when not resent", func(t *testing.T) {
		saved, err := server.store.GetOIDCSettings()
		if err != nil {
			t.Fatal(err)
		}
		if saved.ClientSecretEnc == "" || strings.Contains(saved.ClientSecretEnc, "s3cret-value") {
			t.Fatalf("stored secret %q", saved.ClientSecretEnc)
		}
		without := map[string]any{}
		for k, v := range config {
			if k != "client_secret" {
				without[k] = v
			}
		}
		without["display_name"] = "Renamed"
		if status, v, raw := putSSO(t, srv.URL, adminJWT, without); status != http.StatusOK || !v.ClientSecretSet {
			t.Fatalf("PUT without secret = %d: %s", status, raw)
		}
		after, _ := server.store.GetOIDCSettings()
		if after.ClientSecretEnc != saved.ClientSecretEnc {
			t.Fatal("omitting client_secret replaced the stored one")
		}
	})

	t.Run("the configuration page shows the live issuer", func(t *testing.T) {
		_, raw := apiCall(t, srv.URL, http.MethodGet, "/api/admin/config", adminJWT, nil)
		if !strings.Contains(string(raw), `"key":"sso","value":"`+idp.server.URL) {
			t.Fatalf("config summary = %s", raw)
		}
	})

	t.Run("tests discovery against an issuer", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"issuer_url": idp.server.URL})
		_, raw := apiCall(t, srv.URL, http.MethodPost, "/api/admin/sso/test", adminJWT, body)
		if !strings.Contains(string(raw), `"ok":true`) || !strings.Contains(string(raw), "/authorize") {
			t.Fatalf("test = %s", raw)
		}
		body, _ = json.Marshal(map[string]string{"issuer_url": "http://127.0.0.1:1/nothing"})
		_, raw = apiCall(t, srv.URL, http.MethodPost, "/api/admin/sso/test", adminJWT, body)
		if !strings.Contains(string(raw), `"ok":false`) {
			t.Fatalf("unreachable issuer test = %s", raw)
		}
	})

	t.Run("rejects invalid configurations", func(t *testing.T) {
		for _, body := range []map[string]any{
			{"enabled": true, "client_id": "x"},
			{"enabled": true, "issuer_url": "ftp://idp.example", "client_id": "x"},
			{"enabled": true, "issuer_url": "https://user:pw@idp.example", "client_id": "x"},
			{"enabled": true, "issuer_url": "https://idp.example", "client_id": "x", "default_role": "root"},
			{"enabled": true, "issuer_url": "https://idp.example", "client_id": "x", "groups_claim": "a b"},
		} {
			if status, _, raw := putSSO(t, srv.URL, adminJWT, body); status != http.StatusBadRequest {
				t.Errorf("PUT %v = %d (%s), want 400", body, status, raw)
			}
		}
	})

	t.Run("admins only", func(t *testing.T) {
		createUser(t, srv.URL, adminJWT, "bob", "bob-password", "pusher")
		bob := loginAs(t, srv.URL, "bob", "bob-password")
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/sso", bob, nil); status != http.StatusForbidden {
			t.Fatalf("GET as pusher = %d", status)
		}
		if status, _, _ := putSSO(t, srv.URL, bob, config); status != http.StatusForbidden {
			t.Fatalf("PUT as pusher = %d", status)
		}
	})

	t.Run("disabling takes effect at once", func(t *testing.T) {
		off := map[string]any{"enabled": false, "issuer_url": idp.server.URL, "client_id": "dockyard"}
		if status, v, raw := putSSO(t, srv.URL, adminJWT, off); status != http.StatusOK || v.Enabled {
			t.Fatalf("disable = %d: %s", status, raw)
		}
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/auth/oidc/login", "", nil); status != http.StatusNotFound {
			t.Fatalf("login after disabling = %d, want 404", status)
		}
	})
}

func TestSSOManagedByTheDeployment(t *testing.T) {
	srv := httptest.NewServer(newTestServer(t, func(s *Server) {
		s.auth.EnableOIDC(auth.OIDCConfig{IssuerURL: "https://idp.example", ClientID: "dockyard", ClientSecret: "env-secret"})
	}))
	t.Cleanup(srv.Close)
	adminJWT := loginAs(t, srv.URL, "admin", "test-password")

	status, raw := apiCall(t, srv.URL, http.MethodGet, "/api/admin/sso", adminJWT, nil)
	if status != http.StatusOK || strings.Contains(string(raw), "env-secret") {
		t.Fatalf("GET = %d: %s", status, raw)
	}
	if v := getSSO(t, srv.URL, adminJWT); v.ManagedBy != "env" || !v.Enabled || !v.ClientSecretFromEnv {
		t.Fatalf("view %+v", v)
	}
	if status, _, _ := putSSO(t, srv.URL, adminJWT, map[string]any{"enabled": false}); status != http.StatusConflict {
		t.Fatalf("PUT on a deployment-managed configuration = %d, want 409", status)
	}
}

func TestSSOClientSecretFromTheDeployment(t *testing.T) {
	srv := httptest.NewServer(newTestServer(t, func(s *Server) { s.auth.SetOIDCEnvClientSecret("env-secret") }))
	t.Cleanup(srv.Close)
	adminJWT := loginAs(t, srv.URL, "admin", "test-password")

	base := map[string]any{"enabled": true, "issuer_url": "https://idp.example", "client_id": "dockyard"}
	if status, v, raw := putSSO(t, srv.URL, adminJWT, base); status != http.StatusOK || v.ManagedBy != "console" || !v.ClientSecretFromEnv || !v.ClientSecretSet {
		t.Fatalf("PUT = %d: %s", status, raw)
	}
	base["client_secret"] = "typed-in-the-console"
	if status, _, _ := putSSO(t, srv.URL, adminJWT, base); status != http.StatusBadRequest {
		t.Fatalf("overriding OIDC_CLIENT_SECRET from the console = %d, want 400", status)
	}
}
