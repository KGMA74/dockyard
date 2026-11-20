package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"dockyard/config"
)

type sessionList struct {
	Sessions []struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	} `json:"sessions"`
	CurrentID int64 `json:"current_id"`
}

func mySessions(t *testing.T, base, token string) sessionList {
	t.Helper()
	status, raw := apiCall(t, base, http.MethodGet, "/api/admin/sessions/mine", token, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /sessions/mine = %d: %s", status, raw)
	}
	var list sessionList
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	return list
}

func TestOwnSessions(t *testing.T) {
	srv := httptest.NewServer(newTestServer(t, nil))
	t.Cleanup(srv.Close)
	adminJWT := loginAs(t, srv.URL, "admin", "test-password")
	createUser(t, srv.URL, adminJWT, "alice", "alice-password", "reader")
	createUser(t, srv.URL, adminJWT, "bob", "bob-password", "reader")
	aliceLaptop := loginAs(t, srv.URL, "alice", "alice-password")
	alicePhone := loginAs(t, srv.URL, "alice", "alice-password")
	bob := loginAs(t, srv.URL, "bob", "bob-password")

	t.Run("a reader sees only their own sessions", func(t *testing.T) {
		list := mySessions(t, srv.URL, aliceLaptop)
		if len(list.Sessions) != 2 {
			t.Fatalf("alice has %d sessions listed, want 2", len(list.Sessions))
		}
		for _, s := range list.Sessions {
			if s.Username != "alice" {
				t.Fatalf("alice can see a session of %q", s.Username)
			}
		}
		if list.CurrentID == 0 {
			t.Fatal("current_id missing")
		}
	})

	t.Run("another user's session cannot be revoked, and looks missing", func(t *testing.T) {
		bobSession := mySessions(t, srv.URL, bob).Sessions[0].ID
		path := "/api/admin/sessions/mine/" + strconv.FormatInt(bobSession, 10)
		if status, _ := apiCall(t, srv.URL, http.MethodDelete, path, aliceLaptop, nil); status != http.StatusNotFound {
			t.Fatalf("alice revoking bob's session = %d, want 404", status)
		}
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/repositories", bob, nil); status != http.StatusOK {
			t.Fatalf("bob was signed out by alice's attempt: %d", status)
		}
	})

	t.Run("revoking one's other device signs it out immediately", func(t *testing.T) {
		phoneID := mySessions(t, srv.URL, alicePhone).CurrentID
		path := "/api/admin/sessions/mine/" + strconv.FormatInt(phoneID, 10)
		if status, raw := apiCall(t, srv.URL, http.MethodDelete, path, aliceLaptop, nil); status != http.StatusOK {
			t.Fatalf("revoke own session = %d: %s", status, raw)
		}
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/repositories", alicePhone, nil); status != http.StatusUnauthorized {
			t.Fatalf("revoked device still accepted: %d", status)
		}
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/repositories", aliceLaptop, nil); status != http.StatusOK {
			t.Fatalf("the device that revoked was signed out too: %d", status)
		}
	})

	t.Run("the full session list stays admin-only", func(t *testing.T) {
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/sessions", bob, nil); status != http.StatusForbidden {
			t.Fatalf("reader on /sessions = %d, want 403", status)
		}
	})
}

func TestConfigSummaryIsAdminOnlyAndHasNoSecrets(t *testing.T) {
	cfg := &config.Config{
		RegistryMode:      "mirror",
		RegistryURL:       "https://mirror-user:upstream-secret@registry-1.example.com",
		RegistryPassword:  "upstream-secret",
		StorageBackend:    config.StorageS3,
		StoragePath:       "/data/registry",
		S3Bucket:          "images",
		S3AccessKey:       "AKIA-should-not-leak",
		S3SecretKey:       "s3-secret-should-not-leak",
		AuthPassword:      "admin-password-should-not-leak",
		JWTSecret:         "jwt-secret-should-not-leak",
		TrivyServerURL:    "http://trivy-user:trivy-secret@trivy:4954",
		ScanEnabled:       true,
		RequireSignedPush: true,
		GCGracePeriod:     time.Hour,
	}
	summary := newConfigSummary(cfg, modeMirror, 2, false, cfg.GCGracePeriod.String())

	srv := httptest.NewServer(newTestServer(t, func(s *Server) { s.configSummary = summary }))
	t.Cleanup(srv.Close)
	adminJWT := loginAs(t, srv.URL, "admin", "test-password")
	createUser(t, srv.URL, adminJWT, "carol", "carol-password", "pusher")
	carol := loginAs(t, srv.URL, "carol", "carol-password")

	if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/config", carol, nil); status != http.StatusForbidden {
		t.Fatalf("non-admin on /config = %d, want 403", status)
	}
	status, raw := apiCall(t, srv.URL, http.MethodGet, "/api/admin/config", adminJWT, nil)
	if status != http.StatusOK {
		t.Fatalf("admin on /config = %d: %s", status, raw)
	}
	body := string(raw)
	for _, secret := range []string{"upstream-secret", "mirror-user", "AKIA-should-not-leak", "s3-secret", "admin-password", "jwt-secret", "trivy-secret"} {
		if strings.Contains(body, secret) {
			t.Errorf("config response leaks %q: %s", secret, body)
		}
	}
	for _, want := range []string{`"REQUIRE_SIGNED_PUSH"`, `"images"`, "registry-1.example.com", `"cosign_keys","value":2`} {
		if !strings.Contains(body, want) {
			t.Errorf("config response is missing %s: %s", want, body)
		}
	}
}
