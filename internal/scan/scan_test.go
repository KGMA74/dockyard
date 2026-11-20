package scan

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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

// TestDispatcherStandaloneMode confirms scanning works with TrivyServerURL
// unset (standalone: trivy manages its own vuln DB under TrivyCacheDir).
// The fake trivy binary ignores CLI args either way, so this mainly proves
// nothing in the dispatcher/store path assumes a server URL is set.
func TestDispatcherStandaloneMode(t *testing.T) {
	st := openTestStore(t)
	t.Setenv("HELPER_MODE", "report")
	t.Setenv("HELPER_REPORT", `{"Results":[{"Vulnerabilities":[{"Severity":"LOW"}]}]}`)

	d := newTestDispatcher(t, st, func(c *Config) {
		c.TrivyServerURL = ""
		c.TrivyCacheDir = t.TempDir()
	})

	res, err := d.Enqueue("library/nginx", "latest", "sha256:standalone", "admin")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	got := waitForStatus(t, st, res.Scan.ID, "succeeded")
	if got.LowCount != 1 {
		t.Fatalf("unexpected severity counts: %+v", got)
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

// syncBuffer collects log output written from the dispatcher's goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	logs := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logs
}

func readHelperRecord(t *testing.T, path string) helperRecord {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the fake trivy left no record: %v", err)
	}
	var rec helperRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

// fakeScannerToken stands in for auth.Manager.IssueScannerToken.
type fakeScannerToken struct {
	mu    sync.Mutex
	token string
	repos []string
	ttls  []time.Duration
}

func (f *fakeScannerToken) issue(repo string, ttl time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repos = append(f.repos, repo)
	f.ttls = append(f.ttls, ttl)
	return f.token, nil
}

// Built at run time: a JWT-shaped literal in the source trips secret scanners.
var (
	testScannerToken     = fakeJWT("scanner-token-signature-for-tests")
	testScannerSignature = testScannerToken[strings.LastIndexByte(testScannerToken, '.')+1:]
)

func TestDispatcherHandsTrivyAPerScanRegistryToken(t *testing.T) {
	st := openTestStore(t)
	logs := captureLogs(t)
	out := filepath.Join(t.TempDir(), "trivy-call.json")
	t.Setenv("HELPER_MODE", "report")
	t.Setenv("HELPER_OUT", out)
	// trivy echoing the token in its report must not get it stored.
	t.Setenv("HELPER_REPORT", `{"Results":[{"Target":"`+testScannerToken+`","Vulnerabilities":[{"VulnerabilityID":"CVE-1","Severity":"HIGH"}]}]}`)
	t.Setenv("JWT_SECRET", "dockyard-signing-key-must-not-reach-trivy")

	tokens := &fakeScannerToken{token: testScannerToken}
	d := newTestDispatcher(t, st, func(c *Config) {
		c.Timeout = 3 * time.Second
		c.RegistryToken = tokens.issue
	})
	res, err := d.Enqueue("team/app", "1.0", "sha256:withtoken", "admin")
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, st, res.Scan.ID, "succeeded")

	tokens.mu.Lock()
	if len(tokens.repos) != 1 || tokens.repos[0] != "team/app" {
		t.Fatalf("token requested for %v, want exactly [team/app]", tokens.repos)
	}
	if tokens.ttls[0] != 3*time.Second+scanTokenGrace {
		t.Fatalf("token lifetime = %v, want the scan timeout plus %v", tokens.ttls[0], scanTokenGrace)
	}
	tokens.mu.Unlock()

	rec := readHelperRecord(t, out)
	if !slices.Contains(rec.Env, "TRIVY_REGISTRY_TOKEN="+testScannerToken) {
		t.Fatalf("trivy did not get the token in TRIVY_REGISTRY_TOKEN: %v", rec.Env)
	}
	if strings.Contains(strings.Join(rec.Args, " "), testScannerToken) {
		t.Fatalf("token on trivy's command line: %v", rec.Args)
	}
	if rec.Args[len(rec.Args)-1] != "registry.local/team/app@sha256:withtoken" {
		t.Fatalf("image ref = %q", rec.Args[len(rec.Args)-1])
	}
	for _, kv := range rec.Env {
		if strings.Contains(kv, "dockyard-signing-key") || strings.HasPrefix(kv, "DOCKER_USERNAME=") || strings.HasPrefix(kv, "DOCKER_PASSWORD=") {
			t.Fatalf("trivy inherited %q", kv)
		}
	}

	report, err := st.ScanReport(res.Scan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(report), testScannerSignature) {
		t.Fatalf("stored report carries the token: %s", report)
	}
	if strings.Contains(logs.String(), testScannerSignature) {
		t.Fatalf("logs carry the token:\n%s", logs)
	}
}

func TestDispatcherWithoutTokenSourcePullsAnonymously(t *testing.T) {
	st := openTestStore(t)
	out := filepath.Join(t.TempDir(), "trivy-call.json")
	t.Setenv("HELPER_MODE", "report")
	t.Setenv("HELPER_OUT", out)
	t.Setenv("HELPER_REPORT", `{"Results":[]}`)

	d := newTestDispatcher(t, st, nil) // RegistryToken nil: auth off, or proxy mode
	res, err := d.Enqueue("team/app", "1.0", "sha256:anonymous", "admin")
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, st, res.Scan.ID, "succeeded")
	for _, kv := range readHelperRecord(t, out).Env {
		if strings.HasPrefix(kv, "TRIVY_REGISTRY_TOKEN=") || strings.HasPrefix(kv, "TRIVY_USERNAME=") {
			t.Fatalf("anonymous scan handed trivy %q", kv)
		}
	}
}

func TestDispatcherRefusedTokenFailsClearlyWithoutLeaking(t *testing.T) {
	st := openTestStore(t)
	logs := captureLogs(t)
	t.Setenv("HELPER_MODE", "unauthorized") // echoes the token on stderr

	tokens := &fakeScannerToken{token: testScannerToken}
	d := newTestDispatcher(t, st, func(c *Config) { c.RegistryToken = tokens.issue })
	res, err := d.Enqueue("team/app", "1.0", "sha256:refused", "admin")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForStatus(t, st, res.Scan.ID, "failed")
	if strings.Contains(got.Error, testScannerSignature) {
		t.Fatalf("stored error carries the token: %s", got.Error)
	}
	if !strings.Contains(got.Error, "401") || !strings.Contains(got.Error, "refused the short-lived pull token") {
		t.Fatalf("error = %q, want the 401 and a hint about the scanner's token", got.Error)
	}
	// The failure is logged once the store is updated; wait for that line.
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(logs.String(), "scan: failed") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if l := logs.String(); !strings.Contains(l, "scan: failed") || strings.Contains(l, testScannerSignature) {
		t.Fatalf("want a failure log line without the token, got:\n%s", l)
	}
}
