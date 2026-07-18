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
