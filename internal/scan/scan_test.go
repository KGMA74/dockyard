package scan

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"dockyard/internal/events"
	"dockyard/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "dockyard.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func waitForStatus(t *testing.T, st *store.Store, id int64, want string) *store.ScanResult {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		sc, err := st.ScanByID(id)
		if err != nil {
			t.Fatalf("ScanByID: %v", err)
		}
		if sc.Status == want {
			return sc
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("scan %d did not reach status %q in time", id, want)
	return nil
}

func newTestDispatcher(t *testing.T, st *store.Store, extraCfg func(*Config)) *Dispatcher {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	cfg := Config{
		TrivyBin:       self,
		TrivyServerURL: "http://trivy.local:4954",
		RegistryURL:    "registry.local",
		Timeout:        5 * time.Second,
		DedupWindow:    time.Hour,
	}
	if extraCfg != nil {
		extraCfg(&cfg)
	}
	return NewDispatcher(st, cfg)
}

func TestDispatcherRunsQueuedScanToSuccess(t *testing.T) {
	st := openTestStore(t)
	t.Setenv("HELPER_MODE", "report")
	t.Setenv("HELPER_REPORT", `{"Results":[{"Vulnerabilities":[{"Severity":"HIGH"},{"Severity":"CRITICAL"}]}]}`)

	hub := events.NewHub()
	ch := hub.Subscribe()
	d := newTestDispatcher(t, st, nil)
	d.SetHub(hub)

	res, err := d.Enqueue("library/nginx", "latest", "sha256:aaa", "admin")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if res.Cached {
		t.Fatal("expected a fresh scan, got cached")
	}

	got := waitForStatus(t, st, res.Scan.ID, "succeeded")
	if got.HighCount != 1 || got.CriticalCount != 1 {
		t.Fatalf("unexpected severity counts: %+v", got)
	}

	select {
	case ev := <-ch:
		if ev.Type != "scan" || ev.Name != "library/nginx" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected a scan event on the hub")
	}
}

func TestDispatcherMarksFailureOnTrivyError(t *testing.T) {
	st := openTestStore(t)
	t.Setenv("HELPER_MODE", "fail")

	d := newTestDispatcher(t, st, nil)

	res, err := d.Enqueue("library/redis", "latest", "sha256:bbb", "admin")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	got := waitForStatus(t, st, res.Scan.ID, "failed")
	if got.Error == "" {
		t.Fatal("expected a non-empty error message")
	}
}

func TestDispatcherTimesOut(t *testing.T) {
	st := openTestStore(t)
	t.Setenv("HELPER_MODE", "hang")

	d := newTestDispatcher(t, st, func(c *Config) { c.Timeout = 200 * time.Millisecond })

	res, err := d.Enqueue("library/hang", "latest", "sha256:ccc", "admin")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	waitForStatus(t, st, res.Scan.ID, "failed")
}

func TestDispatcherDedupsWithinWindow(t *testing.T) {
	st := openTestStore(t)
	t.Setenv("HELPER_MODE", "report")
	t.Setenv("HELPER_REPORT", `{"Results":[]}`)

	d := newTestDispatcher(t, st, nil)

	first, err := d.Enqueue("library/nginx", "latest", "sha256:ddd", "admin")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	waitForStatus(t, st, first.Scan.ID, "succeeded")

	second, err := d.Enqueue("library/nginx", "latest", "sha256:ddd", "admin")
	if err != nil {
		t.Fatalf("Enqueue (dedup): %v", err)
	}
	if !second.Cached {
		t.Fatal("expected the second enqueue to be served from cache")
	}
	if second.Scan.ID != first.Scan.ID {
		t.Fatalf("cached scan id = %d, want %d", second.Scan.ID, first.Scan.ID)
	}

	all, total, err := st.ListScans("", "sha256:ddd", 0, 0)
	if err != nil {
		t.Fatalf("ListScans: %v", err)
	}
	if total != 1 || len(all) != 1 {
		t.Fatalf("expected exactly one scan row after dedup, got total=%d len=%d", total, len(all))
	}
}

func TestDispatcherDedupIgnoresDisabledWindow(t *testing.T) {
	st := openTestStore(t)
	t.Setenv("HELPER_MODE", "report")
	t.Setenv("HELPER_REPORT", `{"Results":[]}`)

	d := newTestDispatcher(t, st, func(c *Config) { c.DedupWindow = time.Nanosecond })

	first, err := d.Enqueue("library/nginx", "latest", "sha256:eee", "admin")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	waitForStatus(t, st, first.Scan.ID, "succeeded")

	time.Sleep(10 * time.Millisecond)

	second, err := d.Enqueue("library/nginx", "latest", "sha256:eee", "admin")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if second.Cached {
		t.Fatal("expected a fresh scan once the dedup window has elapsed")
	}
	waitForStatus(t, st, second.Scan.ID, "succeeded")
}

// TestDispatcherDedupAcrossRepos verifies the scan cache is keyed by digest
// alone, not by (name, digest) — the same content pushed under two
// different tags/repos (e.g. a shared base image) reuses one scan.
func TestDispatcherDedupAcrossRepos(t *testing.T) {
	st := openTestStore(t)
	t.Setenv("HELPER_MODE", "report")
	t.Setenv("HELPER_REPORT", `{"Results":[{"Vulnerabilities":[{"Severity":"MEDIUM"}]}]}`)

	d := newTestDispatcher(t, st, nil)

	first, err := d.Enqueue("library/nginx", "latest", "sha256:shared", "admin")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	waitForStatus(t, st, first.Scan.ID, "succeeded")

	second, err := d.Enqueue("mirror/nginx-copy", "v1", "sha256:shared", "someone-else")
	if err != nil {
		t.Fatalf("Enqueue (different repo, same digest): %v", err)
	}
	if !second.Cached {
		t.Fatal("expected the second repo to reuse the cached scan for the shared digest")
	}
	if second.Scan.ID != first.Scan.ID {
		t.Fatalf("cached scan id = %d, want %d", second.Scan.ID, first.Scan.ID)
	}

	all, total, err := st.ListScans("", "sha256:shared", 0, 0)
	if err != nil {
		t.Fatalf("ListScans: %v", err)
	}
	if total != 1 || len(all) != 1 {
		t.Fatalf("expected exactly one scan row shared across repos, got total=%d len=%d", total, len(all))
	}
}
