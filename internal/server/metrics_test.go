package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dockyard/internal/metrics"
)

func TestMetricsEndpoint(t *testing.T) {
	h := newTestServer(t, func(s *Server) { s.metricsEnabled = true })

	// Generate some traffic first.
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodGet, "/v2/some/app/manifests/latest", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics = %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	for _, want := range []string{
		"dockyard_http_requests_total",
		"dockyard_storage_blobs",
		"dockyard_gc_runs_total",
		`path="/v2/{name}/manifests"`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
	// Cardinality guard: the image name must not leak into labels.
	if strings.Contains(string(body), "some/app") {
		t.Error("image name leaked into metric labels")
	}
}

func TestMetricsDisabled(t *testing.T) {
	h := newTestServer(t, nil) // metricsEnabled false by default in tests
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	// Disabled → the SPA fallback answers, not the Prometheus exposition.
	if strings.Contains(rec.Body.String(), "dockyard_http_requests_total") {
		t.Error("metrics served despite METRICS_ENABLED=false")
	}
}

func TestPathClassCardinality(t *testing.T) {
	cases := map[string]string{
		"/v2/":                       "/v2/",
		"/v2/token":                  "/v2/token",
		"/v2/team/app/manifests/v1":  "/v2/{name}/manifests",
		"/v2/a/b/c/blobs/sha256:abc": "/v2/{name}/blobs",
		"/v2/x/blobs/uploads/":       "/v2/{name}/blobs/uploads",
		"/v2/x/blobs/uploads/uuid-1": "/v2/{name}/blobs/uploads",
		"/v2/team/app/tags/list":     "/v2/{name}/tags/list",
		"/v2/_catalog":               "/v2/_catalog",
		"/api/admin/users/bob":       "/api/admin/users/{username}",
		"/api/admin/sessions/42":     "/api/admin/sessions/{id}",
		"/api/admin/repositories":    "/api/admin/repositories",
		"/health":                    "/health",
		"/assets/index-abc123.js":    "/ui-asset",
	}
	for path, want := range cases {
		if got := metrics.PathClass(path); got != want {
			t.Errorf("PathClass(%q) = %q, want %q", path, got, want)
		}
	}
}
