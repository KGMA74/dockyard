package scan

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMain lets this test binary re-exec itself as a fake `trivy` process,
// the same technique the Go standard library uses to test os/exec callers
// without depending on a real external binary (see exec_test.go upstream).
func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		// trivy only inherits an allowlist of variables; the fake one also
		// needs the switches that drive it. Set once, before any test runs.
		trivyPassEnv = append(trivyPassEnv, "GO_WANT_HELPER_PROCESS", "HELPER_MODE", "HELPER_REPORT", "HELPER_OUT", "GOCOVERDIR")
		os.Exit(m.Run())
	}
	runHelperProcess()
	os.Exit(0)
}

// helperRecord is what the fake trivy saw, one JSON line per run appended to
// HELPER_OUT.
type helperRecord struct {
	Args []string `json:"args"`
	Env  []string `json:"env"`
	// DockerConfig is $DOCKER_CONFIG/config.json as the process found it,
	// and DockerConfigMode the permissions of that directory and file.
	DockerConfig     string `json:"docker_config,omitempty"`
	DockerConfigMode string `json:"docker_config_mode,omitempty"`
}

func (r helperRecord) env(key string) (string, bool) {
	for _, kv := range r.Env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v, true
		}
	}
	return "", false
}

// arg returns the value following flag on the command line.
func (r helperRecord) arg(flag string) string {
	if i := slices.Index(r.Args, flag); i >= 0 && i+1 < len(r.Args) {
		return r.Args[i+1]
	}
	return ""
}

// registryToken is what go-containerregistry's keychain would present to
// registry: the registrytoken of its entry in DOCKER_CONFIG, if any.
func (r helperRecord) registryToken(registry string) string {
	var cfg dockerConfig
	_ = json.Unmarshal([]byte(r.DockerConfig), &cfg)
	return cfg.Auths[registry].RegistryToken
}

func recordHelperRun() helperRecord {
	rec := helperRecord{Args: os.Args, Env: os.Environ()}
	if dir := os.Getenv("DOCKER_CONFIG"); dir != "" {
		if raw, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
			rec.DockerConfig = string(raw)
		}
		di, derr := os.Stat(dir)
		fi, ferr := os.Stat(filepath.Join(dir, "config.json"))
		if derr == nil && ferr == nil {
			rec.DockerConfigMode = fmt.Sprintf("%o %o", di.Mode().Perm(), fi.Mode().Perm())
		}
	}
	if out := os.Getenv("HELPER_OUT"); out != "" {
		raw, _ := json.Marshal(rec)
		f, err := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.Write(append(raw, '\n'))
			_ = f.Close()
		}
	}
	return rec
}

// runHelperProcess emulates `trivy` based on env vars set by the test that
// launched this re-exec'd process.
func runHelperProcess() {
	rec := recordHelperRun()
	cacheDir := rec.arg("--cache-dir")

	// DB downloads, as the dispatcher's refresh runs them.
	for flag, dir := range map[string]string{vulnDB: "db", javaDB: "java-db"} {
		if slices.Contains(os.Args, flag) {
			_ = os.MkdirAll(filepath.Join(cacheDir, dir), 0o700)
			_ = os.WriteFile(filepath.Join(cacheDir, dir, "metadata.json"), []byte(`{"Version":1}`), 0o600)
			return
		}
	}

	switch os.Getenv("HELPER_MODE") {
	case "version":
		fmt.Println("Version: 0.56.2-test")
	case "report":
		fmt.Print(os.Getenv("HELPER_REPORT"))
	case "java":
		// A Java image: the jar analyzer needs the Java DB. Trivy's own error
		// when it is missing and may not be downloaded; were the skip flag
		// absent, it would go to ghcr.io for ~1 GB in the middle of the scan.
		switch {
		case !slices.Contains(os.Args, "--skip-java-db-update"):
			fmt.Fprintln(os.Stderr, "FATAL\tFatal error\tjava DB update during the scan: GET https://ghcr.io/v2/aquasecurity/trivy-java-db/manifests/1: DENIED: invalid token")
			os.Exit(1)
		case !fileExists(filepath.Join(cacheDir, "java-db", "metadata.json")):
			fmt.Fprintln(os.Stderr, "FATAL\tFatal error\t'--skip-java-db-update' cannot be specified on the first run")
			os.Exit(1)
		}
		fmt.Print(os.Getenv("HELPER_REPORT"))
	case "unauthorized":
		// What trivy 0.56 prints when the registry refuses its pull, plus
		// the token echoed back, as a careless tool might.
		fmt.Fprintf(os.Stderr, "FATAL\tFatal error\trun error: image scan error: unable to find the specified image %q in [\"docker\" \"containerd\" \"podman\" \"remote\"]:\n"+
			"  * remote error: GET http://localhost:8080/v2/team/app/manifests/sha256:abc: UNAUTHORIZED: invalid token; unexpected status code 401 Unauthorized (token %s)\n",
			os.Args[len(os.Args)-1], rec.registryToken("registry.local"))
		os.Exit(1)
	case "huge":
		for range 10 {
			fmt.Print(strings.Repeat("x", 1024))
		}
	case "fail":
		fmt.Fprintln(os.Stderr, "FATAL: unable to pull image")
		os.Exit(1)
	case "hang":
		time.Sleep(5 * time.Second)
	}
}

// readHelperRuns returns every run the fake trivy recorded in path.
func readHelperRuns(t *testing.T, path string) []helperRecord {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("the fake trivy left no record: %v", err)
	}
	defer func() { _ = f.Close() }()
	var runs []helperRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var rec helperRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatal(err)
		}
		runs = append(runs, rec)
	}
	return runs
}

func isDBDownload(r helperRecord) bool {
	return slices.Contains(r.Args, vulnDB) || slices.Contains(r.Args, javaDB)
}

// scanRun is the one `trivy image <ref>` run among them (not a DB download,
// not --version).
func scanRun(t *testing.T, runs []helperRecord) helperRecord {
	t.Helper()
	var found []helperRecord
	for _, r := range runs {
		if slices.Contains(r.Args, "image") && !isDBDownload(r) {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one scan run, got %d among %d runs", len(found), len(runs))
	}
	return found[0]
}

func isWindows() bool { return runtime.GOOS == "windows" }

func TestBuildTrivyArgsStandalone(t *testing.T) {
	args := buildTrivyArgs("", "/cache", "registry.local/team/app@sha256:abc", false, 0)
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--server") {
		t.Fatalf("standalone args should not include --server: %v", args)
	}
	if !strings.Contains(joined, "--cache-dir /cache") {
		t.Fatalf("expected --cache-dir /cache in args: %v", args)
	}
	if args[len(args)-1] != "registry.local/team/app@sha256:abc" {
		t.Fatalf("image ref should be the last arg: %v", args)
	}
}

func TestBuildTrivyArgsServerMode(t *testing.T) {
	args := buildTrivyArgs("http://trivy:4954", "/cache", "registry.local/team/app@sha256:abc", false, 0)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--server http://trivy:4954") {
		t.Fatalf("expected --server http://trivy:4954 in args: %v", args)
	}
	if !strings.Contains(joined, "--cache-dir /cache") {
		t.Fatalf("expected --cache-dir /cache in args: %v", args)
	}
}

func TestBuildTrivyArgsInsecure(t *testing.T) {
	args := buildTrivyArgs("http://trivy:4954", "/cache", "img", true, 0)
	if !slices.Contains(args, "--insecure") {
		t.Fatalf("expected --insecure in args: %v", args)
	}
	args = buildTrivyArgs("", "/cache", "img", false, 0)
	if slices.Contains(args, "--insecure") {
		t.Fatalf("did not expect --insecure in args: %v", args)
	}
}

func TestBuildTrivyArgsTimeout(t *testing.T) {
	args := buildTrivyArgs("", "/cache", "img", false, 8*time.Minute)
	if !strings.Contains(strings.Join(args, " "), "--timeout 8m0s") {
		t.Fatalf("expected --timeout 8m0s in args: %v", args)
	}
	if strings.Contains(strings.Join(buildTrivyArgs("", "/cache", "img", false, 0), " "), "--timeout") {
		t.Fatal("did not expect --timeout with zero duration")
	}
}

// A scan never downloads a database, online or not: the dispatcher refreshes
// them. The vulnerability DB is a standalone-mode concern only.
func TestBuildTrivyArgsNeverDownloadADatabase(t *testing.T) {
	standalone := buildTrivyArgs("", "/cache", "img", false, 0)
	for _, flag := range []string{"--skip-db-update", "--skip-java-db-update"} {
		if !slices.Contains(standalone, flag) {
			t.Errorf("standalone scan lacks %s: %v", flag, standalone)
		}
	}
	server := buildTrivyArgs("http://trivy:4954", "/cache", "img", false, 0)
	if !slices.Contains(server, "--skip-java-db-update") || slices.Contains(server, "--skip-db-update") {
		t.Errorf("server-mode scan: want --skip-java-db-update only, got %v", server)
	}
}

func TestTrivyErrorHint(t *testing.T) {
	const refused = `GET http://localhost:8080/v2/token?scope=repository%3Aapp%3Apull: unexpected status code 401 Unauthorized: {"error":"authentication required"}`
	cases := []struct {
		stderr    string
		withToken bool
		want      string
	}{
		{"FATAL failed to download vulnerability DB: dial tcp", false, "TRIVY_OFFLINE"},
		{"context deadline exceeded", false, "SCAN_TIMEOUT"},
		{"MANIFEST_UNKNOWN: manifest unknown", true, "still exists"},
		{refused, true, "refused the short-lived pull token"},
		{refused, false, "pulled without credentials"},
		{"DENIED: pull access denied; unexpected status code 403 Forbidden", true, "only the repository being scanned"},
		{"'--skip-java-db-update' cannot be specified on the first run", true, "Java DB is not in TRIVY_CACHE_DIR"},
		{"--skip-db-update cannot be specified on the first run", true, "vulnerability DB is not in TRIVY_CACHE_DIR"},
		{"mkdir /cache/fanal: permission denied", true, ""},
		{"some other unrelated error", false, ""},
	}
	for _, tc := range cases {
		got := trivyErrorHint(tc.stderr, tc.withToken)
		if tc.want == "" {
			if got != "" {
				t.Errorf("trivyErrorHint(%q, %v) = %q, want empty", tc.stderr, tc.withToken, got)
			}
			continue
		}
		if !strings.Contains(got, tc.want) {
			t.Errorf("trivyErrorHint(%q, %v) = %q, want it to mention %q", tc.stderr, tc.withToken, got, tc.want)
		}
	}
}

// The trivy process gets an explicit environment, not Dockyard's: nothing
// in it but the pass-through list, TMPDIR/HOME and what the caller adds.
func TestTrivyEnvIsExplicitAndMinimal(t *testing.T) {
	t.Setenv("JWT_SECRET", "dockyard-signing-key")
	t.Setenv("ADMIN_PASSWORD", "admin-password")
	t.Setenv("REGISTRY_PASSWORD", "upstream-password")
	t.Setenv("TRIVY_PASSWORD", "inherited-trivy-password")
	t.Setenv("TRIVY_REGISTRY_TOKEN", "inherited-registry-token")
	t.Setenv("DOCKER_CONFIG", "/root/.docker")
	t.Setenv("HTTPS_PROXY", "http://proxy.corp:3128")

	cache := "cache"
	env := trivyEnv(cache, "DOCKER_CONFIG=/scan-auth")
	joined := strings.Join(env, "\n")
	for _, leaked := range []string{"dockyard-signing-key", "admin-password", "upstream-password", "inherited-trivy-password", "inherited-registry-token", "/root/.docker"} {
		if strings.Contains(joined, leaked) {
			t.Errorf("trivy environment carries %q:\n%s", leaked, joined)
		}
	}
	for _, want := range []string{"HTTPS_PROXY=http://proxy.corp:3128", "TMPDIR=" + filepath.Join(cache, "tmp"), "HOME=" + cache, "DOCKER_CONFIG=/scan-auth"} {
		if !slices.Contains(env, want) {
			t.Errorf("trivy environment lacks %q:\n%s", want, joined)
		}
	}
}

func TestTrivyCommandKeepsTheTokenOffTheCommandLineAndEnvironment(t *testing.T) {
	token := fakeJWT("signature-of-the-scanner-token")
	dir, err := writeRegistryAuth(t.TempDir(), "localhost:8080", token)
	if err != nil {
		t.Fatal(err)
	}
	cmd := trivyRun{bin: "trivy", cacheDir: "/cache", imageRef: "localhost:8080/team/app@sha256:abc", dockerConfig: dir, secret: token}.command(context.Background())
	if strings.Contains(strings.Join(cmd.Args, " "), token) || strings.Contains(strings.Join(cmd.Env, " "), token) {
		t.Fatalf("token on the command line or in the environment: %v %v", cmd.Args, cmd.Env)
	}
	if !slices.Contains(cmd.Env, "DOCKER_CONFIG="+dir) {
		t.Fatalf("DOCKER_CONFIG not set to the scan's auth dir: %v", cmd.Env)
	}

	anonymous := trivyRun{bin: "trivy", cacheDir: "/cache", imageRef: "img"}.command(context.Background())
	for _, kv := range anonymous.Env {
		if strings.HasPrefix(kv, "TRIVY_REGISTRY_TOKEN=") || strings.HasPrefix(kv, "DOCKER_") {
			t.Fatalf("anonymous pull carries a credential: %v", anonymous.Env)
		}
	}
}

// The auth file names Dockyard's host alone, in the shape the keychain of
// go-containerregistry reads, in a directory only the process can read.
func TestWriteRegistryAuthIsForTheLocalRegistryOnly(t *testing.T) {
	token := fakeJWT("signature-of-the-scanner-token")
	parent := t.TempDir()
	dir, err := writeRegistryAuth(parent, "localhost:8080", token)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]map[string]map[string]string
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg) != 1 || len(cfg["auths"]) != 1 || len(cfg["auths"]["localhost:8080"]) != 1 || cfg["auths"]["localhost:8080"]["registrytoken"] != token {
		t.Fatalf("config.json = %s, want the token for localhost:8080 alone", raw)
	}
	assertPrivate(t, dir)

	second, err := writeRegistryAuth(parent, "localhost:8080", token)
	if err != nil || second == dir {
		t.Fatalf("each scan gets its own directory: %q, %q, %v", dir, second, err)
	}
}

// assertPrivate checks dir is 0700 and its config.json 0600. Windows has no
// such permission bits.
func assertPrivate(t *testing.T, dir string) {
	t.Helper()
	if isWindows() {
		return
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o / %o, want 700 / 600", di.Mode().Perm(), fi.Mode().Perm())
	}
}

func TestResetScratchDirClearsLeftovers(t *testing.T) {
	cache := t.TempDir()
	stale, err := writeRegistryAuth(scratchDir(cache), "localhost:8080", "stale")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratchDir(cache), "trivy-java-db-123.tar.gz"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := resetScratchDir(cache); err != nil {
		t.Fatal(err)
	}
	if fileExists(stale) {
		t.Fatal("an interrupted scan's registry auth survived the restart")
	}
	entries, err := os.ReadDir(scratchDir(cache))
	if err != nil || len(entries) != 0 {
		t.Fatalf("scratch dir after reset: %v, %v — want it empty and present", entries, err)
	}
}

func TestRunTrivyRedactsTheToken(t *testing.T) {
	token := fakeJWT("signature-of-the-scanner-token")
	signature := token[strings.LastIndexByte(token, '.')+1:]
	ctx := context.Background()
	dir, err := writeRegistryAuth(t.TempDir(), "registry.local", token)
	if err != nil {
		t.Fatal(err)
	}

	_, err = runTrivyWithCmd(fakeCommand(ctx, "unauthorized", "DOCKER_CONFIG="+dir), 1<<20, token)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), signature) {
		t.Fatalf("error carries the token: %v", err)
	}
	if !strings.Contains(err.Error(), redacted) || !strings.Contains(err.Error(), "refused the short-lived pull token") {
		t.Fatalf("error = %v, want the redacted stderr and the 401 hint", err)
	}

	report := `{"Results":[],"Echo":"` + token + `","Sig":"` + signature + `"}`
	got, err := runTrivyWithCmd(fakeCommand(ctx, "report", "HELPER_REPORT="+report), 1<<20, token)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), signature) {
		t.Fatalf("report carries the token: %s", got)
	}
}

func TestTallySeverities(t *testing.T) {
	report := []byte(`{
		"Results": [
			{"Vulnerabilities": [
				{"Severity": "CRITICAL"},
				{"Severity": "HIGH"},
				{"Severity": "HIGH"},
				{"Severity": "MEDIUM"},
				{"Severity": "LOW"},
				{"Severity": "unknown"}
			]},
			{"Vulnerabilities": [
				{"Severity": "CRITICAL"}
			]}
		]
	}`)
	counts, err := tallySeverities(report)
	if err != nil {
		t.Fatalf("tallySeverities: %v", err)
	}
	want := severityCounts{Critical: 2, High: 2, Medium: 1, Low: 1, Unknown: 1}
	if counts != want {
		t.Fatalf("tallySeverities = %+v, want %+v", counts, want)
	}
}

func TestTallySeveritiesNoResults(t *testing.T) {
	counts, err := tallySeverities([]byte(`{"Results":[]}`))
	if err != nil {
		t.Fatalf("tallySeverities: %v", err)
	}
	if counts != (severityCounts{}) {
		t.Fatalf("tallySeverities = %+v, want zero", counts)
	}
}

func TestTallySeveritiesInvalidJSON(t *testing.T) {
	if _, err := tallySeverities([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// fakeCommand builds an exec.CommandContext against this same test binary,
// re-exec'd in helper mode.
func fakeCommand(ctx context.Context, mode string, extraEnv ...string) *exec.Cmd {
	self, _ := os.Executable()
	cmd := exec.CommandContext(ctx, self, "-test.run=TestMain")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "HELPER_MODE="+mode)
	cmd.Env = append(cmd.Env, extraEnv...)
	return cmd
}

func TestRunTrivySuccess(t *testing.T) {
	ctx := context.Background()
	report := `{"Results":[{"Vulnerabilities":[{"Severity":"HIGH"}]}]}`
	got, err := runTrivyWithCmd(fakeCommand(ctx, "report", "HELPER_REPORT="+report), 1<<20, "")
	if err != nil {
		t.Fatalf("runTrivyWithCmd: %v", err)
	}
	if string(got) != report {
		t.Fatalf("runTrivyWithCmd = %q, want %q", got, report)
	}
}

func TestRunTrivyFailure(t *testing.T) {
	ctx := context.Background()
	_, err := runTrivyWithCmd(fakeCommand(ctx, "fail"), 1<<20, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unable to pull image") {
		t.Fatalf("err = %v, want stderr message included", err)
	}
}

func TestRunTrivyOversized(t *testing.T) {
	ctx := context.Background()
	_, err := runTrivyWithCmd(fakeCommand(ctx, "huge"), 1024, "") // helper emits 10KB
	if err != errReportTooLarge {
		t.Fatalf("err = %v, want errReportTooLarge", err)
	}
}

func TestRunTrivyTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := runTrivyWithCmd(fakeCommand(ctx, "hang"), 1<<20, "")
	if err == nil {
		t.Fatal("expected error on timeout")
	}
}

func TestTrivyVersion(t *testing.T) {
	ctx := context.Background()
	v, err := trivyVersionWithCmd(fakeCommand(ctx, "version"))
	if err != nil {
		t.Fatalf("trivyVersionWithCmd: %v", err)
	}
	if v != "0.56.2-test" {
		t.Fatalf("trivyVersionWithCmd = %q, want 0.56.2-test", v)
	}
}

// fakeJWT builds a JWT-shaped string (header.payload.signature) that nothing
// signed. It is built at run time so that no token-looking literal sits in
// the source for secret scanners to flag.
func fakeJWT(signature string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"HS256"}`)) + "." + enc([]byte(`{"sub":"dockyard-scanner"}`)) + "." + enc([]byte(signature))
}
