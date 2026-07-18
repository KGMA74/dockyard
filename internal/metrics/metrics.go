// Package metrics exposes Prometheus instrumentation for Dockyard. Collectors
// live on the default registry; dynamic sources (mirror cache, storage stats)
// are wired through swappable callbacks so tests can build many servers
// without duplicate-registration panics.
package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "dockyard_http_requests_total",
		Help: "HTTP requests by method, normalized path and status code.",
	}, []string{"method", "path", "status"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "dockyard_http_request_duration_seconds",
		Help:    "HTTP request latency by method and normalized path.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	authFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dockyard_auth_failures_total",
		Help: "Requests rejected with 401 or 403.",
	})

	gcRuns = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dockyard_gc_runs_total",
		Help: "Garbage collection runs (dry runs excluded).",
	})
	gcReclaimed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dockyard_gc_reclaimed_bytes_total",
		Help: "Bytes reclaimed by garbage collection.",
	})
	gcDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "dockyard_gc_duration_seconds",
		Help:    "Garbage collection run duration.",
		Buckets: []float64{0.1, 0.5, 1, 5, 15, 60, 300},
	})
)

// Swappable sources — read by *Func collectors registered once at init.
var (
	mirrorStatsFn  atomic.Pointer[func() (hits, misses uint64)]
	storageStatsFn atomic.Pointer[func() (blobs int64, bytes int64, repos int64)]
)

func init() {
	promauto.NewCounterFunc(prometheus.CounterOpts{
		Name: "dockyard_mirror_cache_hits_total",
		Help: "Pull-through cache hits (mirror mode).",
	}, func() float64 {
		if fn := mirrorStatsFn.Load(); fn != nil {
			hits, _ := (*fn)()
			return float64(hits)
		}
		return 0
	})
	promauto.NewCounterFunc(prometheus.CounterOpts{
		Name: "dockyard_mirror_cache_misses_total",
		Help: "Pull-through cache misses (mirror mode).",
	}, func() float64 {
		if fn := mirrorStatsFn.Load(); fn != nil {
			_, misses := (*fn)()
			return float64(misses)
		}
		return 0
	})

	newStorageGauge := func(name, help string, pick func(blobs, bytes, repos int64) int64) {
		promauto.NewGaugeFunc(prometheus.GaugeOpts{Name: name, Help: help}, func() float64 {
			if fn := storageStatsFn.Load(); fn != nil {
				blobs, bytes, repos := (*fn)()
				return float64(pick(blobs, bytes, repos))
			}
			return 0
		})
	}
	newStorageGauge("dockyard_storage_blobs", "Number of blobs in storage.",
		func(blobs, _, _ int64) int64 { return blobs })
	newStorageGauge("dockyard_storage_bytes", "Total blob bytes in storage.",
		func(_, bytes, _ int64) int64 { return bytes })
	newStorageGauge("dockyard_storage_repositories", "Number of repositories.",
		func(_, _, repos int64) int64 { return repos })
}

// SetMirrorSource wires the mirror hit/miss counters.
func SetMirrorSource(fn func() (hits, misses uint64)) { mirrorStatsFn.Store(&fn) }

// SetStorageSource wires the storage gauges. The function is called on every
// scrape — give it something cheap (the stats cache lands with P3.2).
func SetStorageSource(fn func() (blobs, bytes, repos int64)) { storageStatsFn.Store(&fn) }

// ObserveRequest records one HTTP request.
func ObserveRequest(method, path string, status int, duration time.Duration) {
	class := PathClass(path)
	httpRequests.WithLabelValues(method, class, strconv.Itoa(status)).Inc()
	httpDuration.WithLabelValues(method, class).Observe(duration.Seconds())
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		authFailures.Inc()
	}
}

// ObserveGC records a (real) garbage collection run.
func ObserveGC(freedBytes int64, duration time.Duration) {
	gcRuns.Inc()
	gcReclaimed.Add(float64(freedBytes))
	gcDuration.Observe(duration.Seconds())
}

// Handler serves the Prometheus scrape endpoint.
func Handler() http.Handler { return promhttp.Handler() }

// PathClass collapses request paths into a bounded label set — image names,
// digests and IDs must never become label values.
func PathClass(p string) string {
	switch {
	case p == "/v2/" || p == "/v2":
		return "/v2/"
	case p == "/v2/token":
		return "/v2/token"
	case p == "/v2/_catalog":
		return "/v2/_catalog"
	case strings.HasPrefix(p, "/v2/"):
		switch {
		case strings.Contains(p, "/blobs/uploads"):
			return "/v2/{name}/blobs/uploads"
		case strings.Contains(p, "/blobs/"):
			return "/v2/{name}/blobs"
		case strings.Contains(p, "/manifests/"):
			return "/v2/{name}/manifests"
		case strings.HasSuffix(p, "/tags/list"):
			return "/v2/{name}/tags/list"
		default:
			return "/v2/other"
		}
	case strings.HasPrefix(p, "/api/admin/users/"):
		return "/api/admin/users/{username}"
	case strings.HasPrefix(p, "/api/admin/sessions/"):
		return "/api/admin/sessions/{id}"
	case strings.HasPrefix(p, "/api/"):
		return p
	case p == "/health" || p == "/metrics" || p == "/":
		return p
	default:
		return "/ui-asset"
	}
}
