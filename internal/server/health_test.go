package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"dockyard/internal/storage"
)

func TestHealthReportsStorage(t *testing.T) {
	h := newTestServer(t, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/health = %d", rec.Code)
	}
	var body struct {
		Status  string `json:"status"`
		Storage struct {
			OK        bool  `json:"ok"`
			LatencyMS int64 `json:"latency_ms"`
			Blobs     *int  `json:"blobs"`
			FreeBytes int64 `json:"free_bytes"`
		} `json:"storage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" || !body.Storage.OK {
		t.Errorf("health = %+v, want ok storage", body)
	}
	if body.Storage.Blobs == nil {
		t.Error("cached stats missing from health")
	}
	if body.Storage.FreeBytes <= 0 {
		t.Error("free_bytes missing for local backend")
	}
}

// countingBackend wraps a Backend counting Stats() calls.
type countingBackend struct {
	storage.Backend
	statsCalls atomic.Int64
}

func (c *countingBackend) Stats() (storage.StorageStats, error) {
	c.statsCalls.Add(1)
	return c.Backend.Stats()
}

func TestStatsCacheAvoidsRepeatedBackendCalls(t *testing.T) {
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	counting := &countingBackend{Backend: backend}
	cache := newStatsCache(counting, time.Hour)

	for range 5 {
		if _, err := cache.Get(); err != nil {
			t.Fatal(err)
		}
	}
	if got := counting.statsCalls.Load(); got != 1 {
		t.Errorf("backend.Stats called %d times through cache, want 1", got)
	}

	// Expired TTL → one refresh.
	cache.ttl = 0
	if _, err := cache.Get(); err != nil {
		t.Fatal(err)
	}
	if got := counting.statsCalls.Load(); got != 2 {
		t.Errorf("backend.Stats after expiry = %d calls, want 2", got)
	}
}
