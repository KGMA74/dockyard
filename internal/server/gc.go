package server

import (
	"fmt"
	"log/slog"
	"time"

	"dockyard/internal/metrics"
	"dockyard/internal/storage"
)

// gcable mirrors admin.gcBackend — keeps the scheduler self-contained.
type gcable interface {
	AllBlobs() ([]string, error)
	BlobSize(digest string) (int64, error)
	ReferencedBlobs() (map[string]struct{}, error)
	RemoveBlob(digest string) error
}

// scheduleMaintenance runs retention then garbage collection every day at
// midnight UTC — retention first, so the blobs it orphans are reclaimed in
// the same pass. No-op if the backend does not implement GC (proxy mode).
func scheduleMaintenance(backend storage.Backend, retention interface{ Run() }) {
	gc, ok := backend.(gcable)
	if !ok {
		return
	}
	slog.Info("maintenance: retention + gc scheduled daily at midnight UTC")
	go func() {
		for {
			time.Sleep(timeUntilMidnightUTC())
			if retention != nil {
				retention.Run()
			}
			runGC(gc)
		}
	}()
}

func runGC(gc gcable) {
	slog.Info("gc: starting scheduled garbage collection")
	start := time.Now()
	referenced, err := gc.ReferencedBlobs()
	if err != nil {
		slog.Error("gc: cannot list referenced blobs", "err", err)
		return
	}
	allBlobs, err := gc.AllBlobs()
	if err != nil {
		slog.Error("gc: cannot list blobs", "err", err)
		return
	}
	var freed int64
	var count int
	for _, digest := range allBlobs {
		if _, ok := referenced[digest]; ok {
			continue
		}
		size, _ := gc.BlobSize(digest)
		if err := gc.RemoveBlob(digest); err == nil {
			freed += size
			count++
		}
	}
	metrics.ObserveGC(freed, time.Since(start))
	slog.Info("gc: done", "removed", count, "freed", gcHumanSize(freed))
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
