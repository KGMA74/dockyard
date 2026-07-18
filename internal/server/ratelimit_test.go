package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"dockyard/internal/auth"
	"dockyard/internal/events"
	"dockyard/internal/storage"
	"dockyard/internal/store"
)

func newTestServer(t *testing.T, mutate func(*Server)) http.Handler {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "dockyard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	mgr, err := auth.New("admin", "test-password", "test-secret", "", dir, st)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := storage.NewLocal(filepath.Join(dir, "registry"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		mode:    modeEmbedded,
		backend: backend,
		auth:    mgr,
		store:   st,
		events:  events.NewHub(),
	}
	if mutate != nil {
		mutate(s)
	}
	return s.RegisterRoutes()
}

func TestLoginRateLimit(t *testing.T) {
	h := newTestServer(t, func(s *Server) { s.rateLimitLoginPerMin = 3 })

	statuses := make([]int, 0, 5)
	for range 5 {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login",
			strings.NewReader(`{"username":"admin","password":"wrong"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "10.1.2.3:5000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		statuses = append(statuses, rec.Code)
	}
	if statuses[0] != http.StatusUnauthorized {
		t.Errorf("first attempt = %d, want 401", statuses[0])
	}
	if statuses[4] != http.StatusTooManyRequests {
		t.Errorf("attempt 5 = %d, want 429 (burst 3); all: %v", statuses[4], statuses)
	}

	// Another IP is unaffected.
	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login",
		strings.NewReader(`{"username":"admin","password":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.9.9.9:5000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("other IP = %d, want 401 (not throttled)", rec.Code)
	}
}

func TestV2TokenRateLimit(t *testing.T) {
	h := newTestServer(t, func(s *Server) { s.rateLimitLoginPerMin = 2 })
	last := 0
	for range 4 {
		req := httptest.NewRequest(http.MethodGet, "/v2/token?service=dockyard-registry", nil)
		req.RemoteAddr = "10.4.4.4:9000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("4th /v2/token = %d, want 429", last)
	}
}

func TestCORSDefaultDeny(t *testing.T) {
	h := newTestServer(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("ACAO = %q with no CORS config, want empty (same-origin only)", got)
	}
}

func TestCORSConfiguredOrigin(t *testing.T) {
	h := newTestServer(t, func(s *Server) { s.corsAllowedOrigins = []string{"https://tools.example"} })
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://tools.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://tools.example" {
		t.Errorf("ACAO = %q, want the configured origin", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got == "https://evil.example" {
		t.Errorf("unlisted origin was allowed")
	}
}
