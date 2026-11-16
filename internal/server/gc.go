package server

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"dockyard/internal/events"
	"dockyard/internal/metrics"
	"dockyard/internal/storage"
)

// staleUploadAge is how long an upload session may sit untouched before the
// nightly maintenance drops it. Generous on purpose: a slow push of a very
// large layer keeps appending, which keeps its session fresh.
const staleUploadAge = 24 * time.Hour

// scheduleMaintenance runs retention, garbage collection and the stale-upload
// purge every day at midnight UTC — retention first, so the blobs it orphans
// are candidates in the same pass (subject to the GC grace period). No-op if
// the backend cannot be collected (proxy mode).
func scheduleMaintenance(backend storage.Backend, retention interface{ Run() }, hub *events.Hub, gcGrace time.Duration) {
	if _, ok := backend.(storage.GCBackend); !ok {
		return
	}
	slog.Info("maintenance: retention + gc scheduled daily at midnight UTC", "gc_grace_period", gcGrace.String())
	go func() {
		for {
			time.Sleep(timeUntilMidnightUTC())
			if retention != nil {
				retention.Run()
			}
			runGC(backend, hub, gcGrace)
			purgeStaleUploads(backend)
		}
	}()
}

func runGC(backend any, hub *events.Hub, grace time.Duration) {
	slog.Info("gc: starting scheduled garbage collection")
	start := time.Now()
	res, err := storage.CollectGarbage(backend, storage.GCOptions{GracePeriod: grace})
	if err != nil {
		if !errors.Is(err, storage.ErrGCUnsupported) {
			// Nothing was deleted: CollectGarbage aborts before the sweep.
			slog.Error("gc: aborted, nothing removed", "err", err)
		}
		return
	}
	metrics.ObserveGC(res.FreedBytes, time.Since(start))
	slog.Info("gc: done",
		"removed", len(res.Removed),
		"freed", gcHumanSize(res.FreedBytes),
		"skipped_recent", res.SkippedRecent,
		"failed", res.Failed,
	)
	if hub != nil && len(res.Removed) > 0 {
		hub.Publish(events.Event{Type: "gc", Actor: "scheduler"})
	}
}

func purgeStaleUploads(backend any) {
	purger, ok := backend.(storage.UploadPurger)
	if !ok {
		return
	}
	removed, err := purger.PurgeStaleUploads(staleUploadAge)
	if err != nil {
		slog.Error("maintenance: stale upload purge failed", "err", err)
		return
	}
	if removed > 0 {
		slog.Info("maintenance: purged abandoned upload sessions", "count", removed)
	}
}

func timeUntilMidnightUTC() time.Duration {
	now := time.Now().UTC()
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return time.Until(next)
}

func gcHumanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
