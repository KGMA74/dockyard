package server

import (
	"sync"
	"time"

	"dockyard/internal/storage"
)

// statsCache memoizes backend.Stats() — on S3 a Stats call lists every object
// in the bucket, and Prometheus scrapes plus /health probes would otherwise
// pay that cost each time.
type statsCache struct {
	backend storage.Backend
	ttl     time.Duration

	mu      sync.Mutex
	fetched time.Time
	cached  storage.StorageStats
	err     error
}

func newStatsCache(backend storage.Backend, ttl time.Duration) *statsCache {
	return &statsCache{backend: backend, ttl: ttl}
}

func (c *statsCache) Get() (storage.StorageStats, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.fetched) < c.ttl {
		return c.cached, c.err
	}
	c.cached, c.err = c.backend.Stats()
	c.fetched = time.Now()
	return c.cached, c.err
}

// sampleStatsLoop snapshots storage stats into stats_history every 6 hours
// (plus once shortly after boot) to feed the insights view.
func (s *Server) sampleStatsLoop() {
	sample := func() {
		if s.stats == nil || s.store == nil {
			return
		}
		if st, err := s.stats.Get(); err == nil {
			_ = s.store.AddStatsSample(st.TotalSize, st.BlobCount, st.RepoCount)
		}
	}
	time.Sleep(30 * time.Second) // let the server settle before the first sample
	sample()
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		sample()
	}
}
