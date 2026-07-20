package scan

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"dockyard/internal/events"
	"dockyard/internal/store"
)

// Config holds the operator-provided settings the dispatcher needs.
type Config struct {
	TrivyBin       string        // path to the trivy binary, e.g. "/trivy"
	TrivyServerURL string        // TRIVY_SERVER_URL — optional; empty = standalone mode (trivy manages its own DB)
	TrivyCacheDir  string        // persistent dir for trivy's vulnerability DB / image cache
	RegistryURL    string        // how trivy reaches Dockyard's own /v2 endpoint
	RegistryUser   string        // credentials trivy uses to pull from Dockyard
	RegistryPass   string
	Insecure       bool          // pass --insecure to trivy (plain HTTP / self-signed local pulls)
	Timeout        time.Duration // per-scan subprocess timeout
	MaxReportBytes int64
	DedupWindow    time.Duration // reuse a recent successful scan instead of re-running
}

// Dispatcher runs at most one trivy scan at a time (image pulls + extraction
// are CPU/disk heavy; unbounded parallelism is a real risk on a small host).
type Dispatcher struct {
	store *store.Store
	cfg   Config
	hub   *events.Hub

	wake     chan struct{}
	interval time.Duration
	mu       sync.Mutex // serializes runNext invocations
}

func NewDispatcher(st *store.Store, cfg Config) *Dispatcher {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Minute
	}
	if cfg.MaxReportBytes <= 0 {
		cfg.MaxReportBytes = 20 << 20
	}
	if cfg.DedupWindow <= 0 {
		cfg.DedupWindow = time.Hour
	}
	d := &Dispatcher{
		store:    st,
		cfg:      cfg,
		wake:     make(chan struct{}, 1),
		interval: 15 * time.Second,
	}
	go d.loop()
	return d
}

// SetHub wires the dispatcher to the in-process event hub so scan completion
// shows up in the SSE feed and fans out to subscribed webhooks.
func (d *Dispatcher) SetHub(hub *events.Hub) { d.hub = hub }

// EnqueueResult reports whether a fresh scan was queued or an existing
// successful one was reused (dedup).
type EnqueueResult struct {
	Scan   *store.ScanResult
	Cached bool
}

// Enqueue queues a scan for name/reference (already resolved to digest by
// the caller). If a successful scan for this digest completed within the
// configured dedup window, that result is returned instead of re-queuing.
func (d *Dispatcher) Enqueue(name, reference, digest, requestedBy string) (*EnqueueResult, error) {
	if latest, err := d.store.LatestScanForDigest(digest); err == nil {
		if finishedAt, ok := parseTime(latest.FinishedAt); ok && time.Since(finishedAt) < d.cfg.DedupWindow {
			return &EnqueueResult{Scan: latest, Cached: true}, nil
		}
	} else if !errors.Is(err, store.ErrScanNotFound) {
		return nil, err
	}

	sc, err := d.store.EnqueueScan(name, reference, digest, requestedBy)
	if err != nil {
		return nil, err
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
	return &EnqueueResult{Scan: sc}, nil
}

func parseTime(s *string) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		// created_at/finished_at use the sqlite strftime format with fractional
		// seconds; RFC3339 parses it too since it's a valid subset, but fall
		// back explicitly in case of drift.
		t, err = time.Parse("2006-01-02T15:04:05.999999999Z", *s)
		if err != nil {
			return time.Time{}, false
		}
	}
	return t, true
}

func (d *Dispatcher) loop() {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-d.wake:
		case <-ticker.C:
		}
		d.runNext()
	}
}

// runNext picks up the oldest queued scan, if any, and runs it to
// completion. The mutex ensures only one trivy subprocess runs at a time.
func (d *Dispatcher) runNext() {
	d.mu.Lock()
	defer d.mu.Unlock()

	sc, err := d.store.NextQueuedScan()
	if err != nil {
		slog.Error("scan: queue query failed", "err", err)
		return
	}
	if sc == nil {
		return
	}
	if err := d.store.MarkScanRunning(sc.ID); err != nil {
		slog.Error("scan: mark running failed", "id", sc.ID, "err", err)
		return
	}

	if err := d.execute(sc); err != nil {
		if merr := d.store.MarkScanFailed(sc.ID, err.Error()); merr != nil {
			slog.Error("scan: mark failed failed", "id", sc.ID, "err", merr)
		}
		slog.Warn("scan: failed", "id", sc.ID, "name", sc.Name, "digest", sc.Digest, "err", err)
		d.publish(sc.Name, sc.Reference, sc.RequestedBy)
		return
	}
	d.publish(sc.Name, sc.Reference, sc.RequestedBy)
}

func (d *Dispatcher) execute(sc *store.ScanResult) error {
	ctx, cancel := context.WithTimeout(context.Background(), d.cfg.Timeout)
	defer cancel()

	imageRef := fmt.Sprintf("%s/%s@%s", d.cfg.RegistryURL, sc.Name, sc.Digest)
	report, err := runTrivy(ctx, d.cfg.TrivyBin, d.cfg.TrivyServerURL, d.cfg.TrivyCacheDir, imageRef, d.cfg.RegistryUser, d.cfg.RegistryPass, d.cfg.Insecure, d.cfg.MaxReportBytes)
	if err != nil {
		return err
	}
	counts, err := tallySeverities(report)
	if err != nil {
		return err
	}
	version, err := trivyVersion(ctx, d.cfg.TrivyBin)
	if err != nil {
		version = ""
	}
	reportGz, err := gzipCompress(report)
	if err != nil {
		return fmt.Errorf("scan: compress report: %w", err)
	}
	return d.store.MarkScanSucceeded(sc.ID, counts.Critical, counts.High, counts.Medium, counts.Low, counts.Unknown, reportGz, version)
}

func (d *Dispatcher) publish(name, reference, actor string) {
	if d.hub == nil {
		return
	}
	d.hub.Publish(events.Event{Type: "scan", Name: name, Tag: reference, Actor: actor})
}

func gzipCompress(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(raw); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
