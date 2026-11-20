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
		// No DB refresh unless a test asks: it would hold the scan lock.
		Offline: true,
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
	cache := t.TempDir()
	out := filepath.Join(t.TempDir(), "trivy-runs.jsonl")
	t.Setenv("HELPER_MODE", "report")
	t.Setenv("HELPER_OUT", out)
	// trivy echoing the token in its report must not get it stored.
	t.Setenv("HELPER_REPORT", `{"Results":[{"Target":"`+testScannerToken+`","Vulnerabilities":[{"VulnerabilityID":"CVE-1","Severity":"HIGH"}]}]}`)
	t.Setenv("JWT_SECRET", "dockyard-signing-key-must-not-reach-trivy")
	t.Setenv("TRIVY_REGISTRY_TOKEN", "a-global-token-must-not-reach-trivy")

	tokens := &fakeScannerToken{token: testScannerToken}
	d := newTestDispatcher(t, st, func(c *Config) {
		c.Timeout = 3 * time.Second
		c.TrivyCacheDir = cache
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

	rec := scanRun(t, readHelperRuns(t, out))
	if got := rec.registryToken("registry.local"); got != testScannerToken {
		t.Fatalf("trivy's docker config has %q for registry.local, want the scan token (config: %s)", got, rec.DockerConfig)
	}
	var cfg dockerConfig
	if err := json.Unmarshal([]byte(rec.DockerConfig), &cfg); err != nil || len(cfg.Auths) != 1 {
		t.Fatalf("docker config = %s, want registry.local alone", rec.DockerConfig)
	}
	if !isWindows() && rec.DockerConfigMode != "700 600" {
		t.Fatalf("docker config permissions = %q, want 700 600", rec.DockerConfigMode)
	}
	dir, _ := rec.env("DOCKER_CONFIG")
	if filepath.Dir(dir) != scratchDir(cache) {
		t.Fatalf("DOCKER_CONFIG = %q, want a directory under %q", dir, scratchDir(cache))
	}
	if fileExists(dir) {
		t.Fatalf("the scan's registry auth %s was not removed", dir)
	}
	if tmp, _ := rec.env("TMPDIR"); tmp != scratchDir(cache) || !fileExists(tmp) {
		t.Fatalf("TMPDIR = %q, want the existing %q", tmp, scratchDir(cache))
	}
	if strings.Contains(strings.Join(rec.Args, " "), testScannerToken) {
		t.Fatalf("token on trivy's command line: %v", rec.Args)
	}
	if rec.Args[len(rec.Args)-1] != "registry.local/team/app@sha256:withtoken" {
		t.Fatalf("image ref = %q", rec.Args[len(rec.Args)-1])
	}
	for _, kv := range rec.Env {
		if strings.HasPrefix(kv, "HELPER_REPORT=") {
			continue // the fake's own switch, carrying the token on purpose
		}
		if strings.Contains(kv, testScannerSignature) || strings.Contains(kv, "must-not-reach-trivy") || strings.HasPrefix(kv, "DOCKER_USERNAME=") {
			t.Fatalf("trivy's environment carries %q", kv)
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
	out := filepath.Join(t.TempDir(), "trivy-runs.jsonl")
	t.Setenv("HELPER_MODE", "report")
	t.Setenv("HELPER_OUT", out)
	t.Setenv("HELPER_REPORT", `{"Results":[]}`)

	d := newTestDispatcher(t, st, nil) // RegistryToken nil: auth off, or proxy mode
	res, err := d.Enqueue("team/app", "1.0", "sha256:anonymous", "admin")
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, st, res.Scan.ID, "succeeded")
	for _, kv := range scanRun(t, readHelperRuns(t, out)).Env {
		if strings.HasPrefix(kv, "DOCKER_CONFIG=") || strings.HasPrefix(kv, "TRIVY_REGISTRY_TOKEN=") || strings.HasPrefix(kv, "TRIVY_USERNAME=") {
			t.Fatalf("anonymous scan handed trivy %q", kv)
		}
	}
}

func TestDispatcherRefusedTokenFailsClearlyWithoutLeaking(t *testing.T) {
	st := openTestStore(t)
	logs := captureLogs(t)
	cache := t.TempDir()
	t.Setenv("HELPER_MODE", "unauthorized") // echoes the token on stderr

	tokens := &fakeScannerToken{token: testScannerToken}
	d := newTestDispatcher(t, st, func(c *Config) {
		c.TrivyCacheDir = cache
		c.RegistryToken = tokens.issue
	})
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
	// A failed scan cleans up its registry auth as well.
	if entries, _ := os.ReadDir(scratchDir(cache)); len(entries) != 0 {
		t.Fatalf("scratch dir after a failed scan: %v", entries)
	}
}

// Online, the dispatcher downloads both databases itself before the first
// scan — without any credential — and the scan of a Java image then runs
// with both skip flags and the Java DB already in the cache: nothing for it
// to fetch from ghcr.io.
func TestJavaImageScanUsesTheDatabasesTheDispatcherDownloaded(t *testing.T) {
	st := openTestStore(t)
	cache := t.TempDir()
	out := filepath.Join(t.TempDir(), "trivy-runs.jsonl")
	t.Setenv("HELPER_MODE", "java")
	t.Setenv("HELPER_OUT", out)
	t.Setenv("HELPER_REPORT", `{"Results":[{"Target":"app.jar","Vulnerabilities":[{"VulnerabilityID":"CVE-2021-44228","Severity":"CRITICAL"}]}]}`)

	tokens := &fakeScannerToken{token: testScannerToken}
	d := newTestDispatcher(t, st, func(c *Config) {
		c.TrivyServerURL = "" // standalone
		c.TrivyCacheDir = cache
		c.Offline = false
		c.RegistryToken = tokens.issue
	})
	// Queued at once: it must wait for the databases, not race them.
	res, err := d.Enqueue("team/java-app", "1.0", "sha256:java", "admin")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForStatus(t, st, res.Scan.ID, "succeeded")
	if got.CriticalCount != 1 {
		t.Fatalf("counts = %+v", got)
	}

	runs := readHelperRuns(t, out)
	var downloads []string
	for i, r := range runs {
		if !isDBDownload(r) {
			continue
		}
		for _, flag := range []string{vulnDB, javaDB} {
			if slices.Contains(r.Args, flag) {
				downloads = append(downloads, flag)
			}
		}
		if _, set := r.env("DOCKER_CONFIG"); set || r.DockerConfig != "" {
			t.Errorf("DB download %d ran with registry auth: %v", i, r.Env)
		}
	}
	if !slices.Equal(downloads, []string{vulnDB, javaDB}) {
		t.Fatalf("DB downloads = %v, want the vulnerability DB then the Java DB", downloads)
	}
	scan := scanRun(t, runs)
	if scanIdx := slices.IndexFunc(runs, func(r helperRecord) bool { return !isDBDownload(r) && slices.Contains(r.Args, "image") }); scanIdx < 2 {
		t.Fatalf("the scan ran before the DB downloads finished (run %d)", scanIdx)
	}
	for _, flag := range []string{"--skip-db-update", "--skip-java-db-update"} {
		if !slices.Contains(scan.Args, flag) {
			t.Errorf("scan ran without %s: %v", flag, scan.Args)
		}
	}
}

// Offline (or a failed download), nothing is fetched and a Java scan says
// plainly what is missing instead of reaching for ghcr.io.
func TestJavaImageScanWithoutJavaDBExplainsWhy(t *testing.T) {
	st := openTestStore(t)
	t.Setenv("HELPER_MODE", "java")
	d := newTestDispatcher(t, st, func(c *Config) { c.TrivyCacheDir = t.TempDir() }) // offline, empty cache
	res, err := d.Enqueue("team/java-app", "1.0", "sha256:nojavadb", "admin")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForStatus(t, st, res.Scan.ID, "failed")
	if !strings.Contains(got.Error, "Java DB is not in TRIVY_CACHE_DIR") {
		t.Fatalf("error = %q, want the Java DB hint", got.Error)
	}
}
