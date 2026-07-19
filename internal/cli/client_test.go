package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigRoundTrip(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir()) // Windows home
	t.Setenv("HOME", t.TempDir())        // Unix home

	cfg := &Config{Server: "http://reg.example", Token: "tok", RefreshToken: "ref"}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if *loaded != *cfg {
		t.Errorf("round trip = %+v, want %+v", loaded, cfg)
	}
}

func TestLoadConfigWithoutLogin(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "login") {
		t.Errorf("expected a helpful not-logged-in error, got %v", err)
	}
}

// TestSilentRefreshOn401: an expired access token triggers one refresh and a
// retry, transparently to the command.
func TestSilentRefreshOn401(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/admin/auth/refresh":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"fresh","refresh_token":"next-ref"}`))
		case r.Header.Get("Authorization") == "Bearer fresh":
			calls++
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &Config{Server: srv.URL, Token: "stale", RefreshToken: "ref"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	c := NewClient(cfg)
	var out struct {
		OK bool `json:"ok"`
	}
	if err := c.GetJSON("/api/admin/repositories", &out); err != nil {
		t.Fatalf("GetJSON through refresh: %v", err)
	}
	if !out.OK || calls != 1 {
		t.Errorf("ok=%v calls=%d, want true/1", out.OK, calls)
	}
	if cfg.Token != "fresh" || cfg.RefreshToken != "next-ref" {
		t.Errorf("tokens not rotated: %+v", cfg)
	}
}
