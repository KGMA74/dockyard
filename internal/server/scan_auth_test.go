package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"dockyard/internal/auth"
	"dockyard/internal/store"
)

// TestMain lets this test binary stand in for trivy: the scan dispatcher
// runs it with trivy's own arguments ("image ...", "--cache-dir ... image
// --download-db-only", "--version"), which no `go test` invocation starts
// with. It gets only the environment Dockyard gives trivy, so it is driven by
// those arguments alone.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && (os.Args[1] == "image" || os.Args[1] == "--version" || os.Args[1] == "--cache-dir") {
		os.Exit(fakeTrivy(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeTrivyRun is what the fake trivy saw, one JSON line per run appended
// to fakeTrivyRecord in its cache dir.
type fakeTrivyRun struct {
	Args []string `json:"args"`
	Env  []string `json:"env"`
	// DockerConfig is $DOCKER_CONFIG/config.json as trivy found it.
	DockerConfig string `json:"docker_config,omitempty"`
}

const fakeTrivyRecord = "fake-trivy-runs.jsonl"

func (r fakeTrivyRun) env(key string) string {
	for _, kv := range r.Env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}
	return ""
}

func (r fakeTrivyRun) isDBDownload() bool {
	return slices.Contains(r.Args, "--download-db-only") || slices.Contains(r.Args, "--download-java-db-only")
}

// auths is the registries the docker config holds credentials for.
func (r fakeTrivyRun) auths() map[string]struct {
	RegistryToken string `json:"registrytoken"`
} {
	var cfg struct {
		Auths map[string]struct {
			RegistryToken string `json:"registrytoken"`
		} `json:"auths"`
	}
	_ = json.Unmarshal([]byte(r.DockerConfig), &cfg)
	return cfg.Auths
}

// fakeTrivy behaves like trivy 0.56 as far as Dockyard can tell:
//   - --download-db-only / --download-java-db-only fill the cache;
//   - a scan pulls the image the way go-containerregistry does with a
//     registrytoken in DOCKER_CONFIG: an anonymous ping of /v2/, then the
//     manifest and every blob with that token as a Bearer header, never
//     asking /v2/token, and only for the registry the config names;
//   - a scan without --skip-db-update / --skip-java-db-update would update a
//     database from ghcr.io in the middle of it; with the flag and no
//     database in the cache, it fails as trivy does. A repository named
//     *java* has jars, so it needs the Java DB.
func fakeTrivy(args []string) int {
	if args[0] == "--version" {
		fmt.Println("Version: 0.56.2-fake")
		return 0
	}
	cacheDir := args[slices.Index(args, "--cache-dir")+1]
	run := fakeTrivyRun{Args: os.Args, Env: os.Environ()}
	if dir := os.Getenv("DOCKER_CONFIG"); dir != "" {
		raw, _ := os.ReadFile(filepath.Join(dir, "config.json"))
		run.DockerConfig = string(raw)
	}
	if line, err := json.Marshal(run); err == nil {
		if f, err := os.OpenFile(filepath.Join(cacheDir, fakeTrivyRecord), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			_, _ = f.Write(append(line, '\n'))
			_ = f.Close()
		}
	}

	for flag, dir := range map[string]string{"--download-db-only": "db", "--download-java-db-only": "java-db"} {
		if slices.Contains(args, flag) {
			_ = os.MkdirAll(filepath.Join(cacheDir, dir), 0o700)
			_ = os.WriteFile(filepath.Join(cacheDir, dir, "metadata.json"), []byte(`{"Version":2}`), 0o600)
			return 0
		}
	}

	ref := args[len(args)-1] // localhost:<port>/<name>@<digest>
	host, rest, _ := strings.Cut(ref, "/")
	name, digest, _ := strings.Cut(rest, "@")
	fail := func(err error) int {
		fmt.Fprintf(os.Stderr, "FATAL\tFatal error\trun error: image scan error: %v\n", err)
		return 1
	}

	dbs := map[string]string{"--skip-db-update": "db"}
	if strings.Contains(name, "java") {
		dbs["--skip-java-db-update"] = "java-db"
	}
	for skip, dir := range dbs {
		if !slices.Contains(args, skip) {
			// Would update it from ghcr.io now, during the scan.
			if _, err := http.Get("https://ghcr.io/v2/aquasecurity/trivy-" + dir + "/manifests/1"); err != nil {
				return fail(fmt.Errorf("%s update during the scan: %w", dir, err))
			}
			return fail(fmt.Errorf("%s update during the scan: GET https://ghcr.io/...: DENIED: invalid token", dir))
		}
		if _, err := os.Stat(filepath.Join(cacheDir, dir, "metadata.json")); err != nil {
			return fail(fmt.Errorf("'%s' cannot be specified on the first run", skip))
		}
	}

	token := run.auths()[host].RegistryToken
	base := "http://" + host + "/v2/"
	get := func(url string, bearer bool) ([]byte, error) {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
		if bearer && token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %s: unexpected status code %d %s: %s", url, resp.StatusCode, http.StatusText(resp.StatusCode), body)
		}
		return body, nil
	}

	if _, err := get(base, false); err == nil || !strings.Contains(err.Error(), "401") {
		return fail(fmt.Errorf("ping: expected a 401 challenge, got %v", err))
	}
	raw, err := get(base+name+"/manifests/"+digest, true)
	if err != nil {
		return fail(err)
	}
	var manifest struct {
		Config struct{ Digest string }   `json:"config"`
		Layers []struct{ Digest string } `json:"layers"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return fail(err)
	}
	blobs := []string{manifest.Config.Digest}
	for _, l := range manifest.Layers {
		blobs = append(blobs, l.Digest)
	}
	for _, b := range blobs {
		if _, err := get(base+name+"/blobs/"+b, true); err != nil {
			return fail(err)
		}
	}
	fmt.Print(`{"Results":[{"Target":"` + name + `","Vulnerabilities":[{"VulnerabilityID":"CVE-2024-1111","Severity":"HIGH"}]}]}`)
	return 0
}

func readFakeTrivyRuns(t *testing.T, cacheDir string) []fakeTrivyRun {
	t.Helper()
	f, err := os.Open(filepath.Join(cacheDir, fakeTrivyRecord))
	if err != nil {
		t.Fatalf("trivy never ran: %v", err)
	}
	defer func() { _ = f.Close() }()
	var runs []fakeTrivyRun
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var r fakeTrivyRun
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		runs = append(runs, r)
	}
	return runs
}

// imageScans returns the runs that scanned an image (not DB downloads).
func imageScans(runs []fakeTrivyRun) []fakeTrivyRun {
	var out []fakeTrivyRun
	for _, r := range runs {
		if !r.isDBDownload() {
			out = append(out, r)
		}
	}
	return out
}

type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func captureServerLogs(t *testing.T) *lockedBuffer {
	t.Helper()
	logs := &lockedBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logs
}

// startScanningServer runs a Dockyard with registry auth on, anonymous pull
// off and scanning on, this test binary as its trivy, listening where trivy
// pulls from (localhost:<port>). It returns the server, its store and the
// "localhost:<port>" trivy pulls from.
func startScanningServer(t *testing.T, cacheDir string, mutate func(*Server)) (*httptest.Server, *store.Store, string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	var st *store.Store
	h := newTestServer(t, func(s *Server) {
		s.v2AuthEnabled = true
		s.v2AnonymousPull = false
		s.scanEnabled = true
		s.trivyBinPath = self
		s.trivyCacheDir = cacheDir
		s.port = port
		s.scanTimeout = 30 * time.Second
		st = s.store
		if mutate != nil {
			mutate(s)
		}
	})
	srv := httptest.NewUnstartedServer(h)
	_ = srv.Listener.Close()
	srv.Listener = listener
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, st, fmt.Sprintf("localhost:%d", port)
}

// scanAndWait triggers a scan as admin and waits for it to finish.
func scanAndWait(t *testing.T, base, adminJWT, name, reference string) store.ScanResult {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name, "reference": reference})
	status, raw := apiCall(t, base, http.MethodPost, "/api/admin/scans", adminJWT, body)
	if status != http.StatusAccepted {
		t.Fatalf("trigger scan = %d: %s", status, raw)
	}
	var queued struct {
		Scan store.ScanResult `json:"scan"`
	}
	if err := json.Unmarshal(raw, &queued); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		var sc store.ScanResult
		_, raw = apiCall(t, base, http.MethodGet, fmt.Sprintf("/api/admin/scans/%d", queued.Scan.ID), adminJWT, nil)
		if err := json.Unmarshal(raw, &sc); err != nil {
			t.Fatal(err)
		}
		if sc.Status == "succeeded" || sc.Status == "failed" {
			return sc
		}
		if time.Now().After(deadline) {
			t.Fatalf("scan still %q", sc.Status)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// seedVulnDB stands in for the DB the image ships and the server seeds.
func seedVulnDB(t *testing.T, cacheDir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(cacheDir, "db"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "db", "metadata.json"), []byte(`{"Version":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// assertScratchDirEmpty checks no scan left its registry auth behind.
func assertScratchDirEmpty(t *testing.T, cacheDir string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(cacheDir, "tmp"))
	if err != nil {
		t.Fatalf("trivy's scratch dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("left in trivy's scratch dir after the scan: %v", entries)
	}
}

// The regression from 0.6.1: with V2_AUTH_ENABLED=true and anonymous pull
// off, trivy was handed credentials it does not read, pulled anonymously
// and got a 401 on every scan.
func TestScanWithRegistryAuthUsesAPerScanToken(t *testing.T) {
	// Dockyard's own environment must not reach trivy.
	t.Setenv("JWT_SECRET", "jwt-secret-from-dockyard-env")
	t.Setenv("ADMIN_PASSWORD", "admin-password-from-dockyard-env")
	logs := captureServerLogs(t)
	cacheDir := t.TempDir()
	seedVulnDB(t, cacheDir)
	srv, st, registry := startScanningServer(t, cacheDir, func(s *Server) {
		s.trivyOffline = true // no DB refresh
	})

	adminJWT := loginAs(t, srv.URL, "admin", "test-password")
	pushToken := fetchToken(t, srv.URL, "admin", "test-password")
	pushImage(t, srv.URL, pushToken, "team/app", "1.0")
	pushImage(t, srv.URL, pushToken, "team/other", "1.0")

	sc := scanAndWait(t, srv.URL, adminJWT, "team/app", "1.0")
	if sc.Status != "succeeded" || sc.HighCount != 1 {
		t.Fatalf("scan = %s (high=%d), error: %s", sc.Status, sc.HighCount, sc.Error)
	}

	scans := imageScans(readFakeTrivyRuns(t, cacheDir))
	if len(scans) != 1 {
		t.Fatalf("want one trivy scan, got %d", len(scans))
	}
	call := scans[0]
	auths := call.auths()
	token := auths[registry].RegistryToken
	if token == "" || len(auths) != 1 {
		t.Fatalf("trivy's docker config = %s, want a token for %s alone", call.DockerConfig, registry)
	}
	for _, kv := range call.Env {
		if strings.Contains(kv, "from-dockyard-env") || strings.HasPrefix(kv, "TRIVY_REGISTRY_TOKEN=") || strings.Contains(kv, token) {
			t.Errorf("trivy's environment carries %q", kv)
		}
	}
	if strings.Contains(strings.Join(call.Args, " "), token) {
		t.Fatalf("token on trivy's command line: %v", call.Args)
	}
	if _, err := os.Stat(call.env("DOCKER_CONFIG")); !os.IsNotExist(err) {
		t.Fatalf("the scan's DOCKER_CONFIG %s still exists (%v)", call.env("DOCKER_CONFIG"), err)
	}
	assertScratchDirEmpty(t, cacheDir)

	t.Run("the token is the scanner's, for this repository, read-only", func(t *testing.T) {
		digest := imageDigest("team/app", "1.0")
		checks := []struct {
			name, method, path string
			want               int
		}{
			{"pull the scanned image", http.MethodGet, "/v2/team/app/manifests/" + digest, http.StatusOK},
			{"pull another repository", http.MethodGet, "/v2/team/other/manifests/1.0", http.StatusForbidden},
			{"push to the scanned repository", http.MethodPut, "/v2/team/app/manifests/2.0", http.StatusForbidden},
			{"delete from the scanned repository", http.MethodDelete, "/v2/team/app/manifests/" + digest, http.StatusForbidden},
		}
		for _, c := range checks {
			if got := authedStatus(t, srv.URL, c.method, c.path, token, nil); got != c.want {
				t.Errorf("%s = %d, want %d", c.name, got, c.want)
			}
		}
		status, raw := apiCall(t, srv.URL, http.MethodGet, "/v2/_catalog", token, nil)
		if status != http.StatusOK || strings.Contains(string(raw), "team/other") {
			t.Errorf("catalog = %d %s, want team/app alone", status, raw)
		}
		if status, _ := apiCall(t, srv.URL, http.MethodGet, "/api/admin/repositories", token, nil); status != http.StatusForbidden {
			t.Errorf("admin API with the scanner token = %d, want 403", status)
		}
	})

	t.Run("the token is in neither the stored scan nor the logs", func(t *testing.T) {
		signature := token[strings.LastIndexByte(token, '.')+1:]
		_, report := apiCall(t, srv.URL, http.MethodGet, fmt.Sprintf("/api/admin/scans/%d/report", sc.ID), adminJWT, nil)
		stored, _ := st.ScanByID(sc.ID)
		for where, text := range map[string]string{"report": string(report), "scan error": stored.Error, "logs": logs.String()} {
			if strings.Contains(text, signature) {
				t.Errorf("%s carries the scanner token", where)
			}
		}
	})
}

// 0.7.1 still failed on Java images: the token, global to trivy, also went
// to ghcr.io for the Java DB (DENIED), and the scan was the one downloading
// it. Now Dockyard downloads both databases itself, with no credential,
// before the first scan; the scan of a Java image then needs nothing from
// ghcr.io — here, unreachable through a dead proxy.
func TestJavaImageScanNeedsNoGhcrAccess(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9") // ghcr.io unreachable during the scan
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	cacheDir := t.TempDir() // empty: no DB at all yet
	srv, _, registry := startScanningServer(t, cacheDir, func(s *Server) {
		s.trivyOffline = false // the dispatcher downloads the DBs at startup
	})

	adminJWT := loginAs(t, srv.URL, "admin", "test-password")
	pushToken := fetchToken(t, srv.URL, "admin", "test-password")
	pushImage(t, srv.URL, pushToken, "team/java-app", "1.0")

	sc := scanAndWait(t, srv.URL, adminJWT, "team/java-app", "1.0")
	if sc.Status != "succeeded" || sc.HighCount != 1 {
		t.Fatalf("Java image scan = %s, error: %s", sc.Status, sc.Error)
	}

	runs := readFakeTrivyRuns(t, cacheDir)
	var downloads []string
	for _, r := range runs {
		if r.isDBDownload() {
			downloads = append(downloads, r.Args[len(r.Args)-2]) // ... image <flag> --quiet
			if r.env("DOCKER_CONFIG") != "" || r.DockerConfig != "" {
				t.Errorf("a DB download ran with registry credentials: %v", r.Env)
			}
		}
	}
	if !slices.Equal(downloads, []string{"--download-db-only", "--download-java-db-only"}) {
		t.Fatalf("DB downloads = %v, want the vulnerability DB then the Java DB", downloads)
	}
	scans := imageScans(runs)
	if len(scans) != 1 {
		t.Fatalf("want one trivy scan, got %d", len(scans))
	}
	scan := scans[0]
	for _, flag := range []string{"--skip-db-update", "--skip-java-db-update"} {
		if !slices.Contains(scan.Args, flag) {
			t.Errorf("scan ran without %s", flag)
		}
	}
	if auths := scan.auths(); len(auths) != 1 || auths[registry].RegistryToken == "" {
		t.Fatalf("scan's docker config = %s, want a token for %s alone", scan.DockerConfig, registry)
	}
	if got := scan.env("TMPDIR"); got != filepath.Join(cacheDir, "tmp") {
		t.Fatalf("TMPDIR = %q, want a subdirectory of the cache", got)
	}
	assertScratchDirEmpty(t, cacheDir)
}

func TestScannerTokenOnlyInEmbeddedModeWithRegistryAuth(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "dockyard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	mgr, err := auth.New("admin", "test-password", "test-secret", "", t.TempDir(), st)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		mode      mode
		v2Auth    bool
		wantToken bool
	}{
		{"embedded, auth off", modeEmbedded, false, false},
		{"embedded, auth on", modeEmbedded, true, true},
		{"proxy, auth off", modeProxy, false, false},
		{"proxy, auth on: upstream credentials apply", modeProxy, true, false},
	}
	for _, tc := range cases {
		s := &Server{mode: tc.mode, v2AuthEnabled: tc.v2Auth, auth: mgr}
		if got := s.scannerTokenSource() != nil; got != tc.wantToken {
			t.Errorf("%s: token source = %v, want %v", tc.name, got, tc.wantToken)
		}
	}
}
