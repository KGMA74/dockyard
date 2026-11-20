package scan

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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

// helperRecord is what the fake trivy saw, written to HELPER_OUT.
type helperRecord struct {
	Args []string `json:"args"`
	Env  []string `json:"env"`
}

// runHelperProcess emulates `trivy` based on env vars set by the test that
// launched this re-exec'd process.
func runHelperProcess() {
	if out := os.Getenv("HELPER_OUT"); out != "" && slices.Contains(os.Args, "image") {
		raw, _ := json.Marshal(helperRecord{Args: os.Args, Env: os.Environ()})
		_ = os.WriteFile(out, raw, 0o600)
	}
	switch os.Getenv("HELPER_MODE") {
	case "version":
		fmt.Println("Version: 0.56.2-test")
	case "report":
		fmt.Print(os.Getenv("HELPER_REPORT"))
	case "unauthorized":
		// What trivy 0.56 prints when the registry refuses its pull, plus
		// the token echoed back, as a careless tool might.
		fmt.Fprintf(os.Stderr, "FATAL\tFatal error\trun error: image scan error: unable to find the specified image %q in [\"docker\" \"containerd\" \"podman\" \"remote\"]:\n"+
			"  * remote error: GET http://localhost:8080/v2/team/app/manifests/sha256:abc: UNAUTHORIZED: invalid token; unexpected status code 401 Unauthorized (token %s)\n",
			os.Args[len(os.Args)-1], os.Getenv("TRIVY_REGISTRY_TOKEN"))
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

func TestBuildTrivyArgsStandalone(t *testing.T) {
	args := buildTrivyArgs("", "/cache", "registry.local/team/app@sha256:abc", false, false, 0)
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
	args := buildTrivyArgs("http://trivy:4954", "/cache", "registry.local/team/app@sha256:abc", false, false, 0)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--server http://trivy:4954") {
		t.Fatalf("expected --server http://trivy:4954 in args: %v", args)
	}
	if !strings.Contains(joined, "--cache-dir /cache") {
		t.Fatalf("expected --cache-dir /cache in args: %v", args)
	}
}

func TestBuildTrivyArgsInsecure(t *testing.T) {
	args := buildTrivyArgs("http://trivy:4954", "/cache", "img", true, false, 0)
	if !slices.Contains(args, "--insecure") {
		t.Fatalf("expected --insecure in args: %v", args)
	}
	args = buildTrivyArgs("", "/cache", "img", false, false, 0)
	if slices.Contains(args, "--insecure") {
		t.Fatalf("did not expect --insecure in args: %v", args)
	}
}

func TestBuildTrivyArgsTimeoutAndOffline(t *testing.T) {
	args := buildTrivyArgs("", "/cache", "img", false, true, 8*time.Minute)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--timeout 8m0s") {
		t.Fatalf("expected --timeout 8m0s in args: %v", args)
	}
	if !slices.Contains(args, "--skip-db-update") {
		t.Fatalf("expected --skip-db-update in offline args: %v", args)
	}

	online := buildTrivyArgs("", "/cache", "img", false, false, 0)
	if slices.Contains(online, "--skip-db-update") {
		t.Fatalf("did not expect --skip-db-update when online: %v", online)
	}
	if strings.Contains(strings.Join(online, " "), "--timeout") {
		t.Fatalf("did not expect --timeout with zero duration: %v", online)
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
	t.Setenv("HTTPS_PROXY", "http://proxy.corp:3128")

	env := trivyEnv("/cache", "TRIVY_REGISTRY_TOKEN=tok")
	joined := strings.Join(env, "\n")
	for _, leaked := range []string{"dockyard-signing-key", "admin-password", "upstream-password", "inherited-trivy-password"} {
		if strings.Contains(joined, leaked) {
			t.Errorf("trivy environment carries %q:\n%s", leaked, joined)
		}
	}
	for _, want := range []string{"HTTPS_PROXY=http://proxy.corp:3128", "TMPDIR=/cache", "HOME=/cache", "TRIVY_REGISTRY_TOKEN=tok"} {
		if !slices.Contains(env, want) {
			t.Errorf("trivy environment lacks %q:\n%s", want, joined)
		}
	}
}

func TestTrivyCommandKeepsTheTokenOffTheCommandLine(t *testing.T) {
	token := fakeJWT("signature-of-the-scanner-token")
	cmd := trivyRun{bin: "trivy", cacheDir: "/cache", imageRef: "localhost:8080/team/app@sha256:abc", registryToken: token}.command(context.Background())
	if strings.Contains(strings.Join(cmd.Args, " "), token) {
		t.Fatalf("token on the command line: %v", cmd.Args)
	}
	if !slices.Contains(cmd.Env, "TRIVY_REGISTRY_TOKEN="+token) {
		t.Fatalf("token not in the child environment: %v", cmd.Env)
	}

	anonymous := trivyRun{bin: "trivy", cacheDir: "/cache", imageRef: "img"}.command(context.Background())
	for _, kv := range anonymous.Env {
		if strings.HasPrefix(kv, "TRIVY_REGISTRY_TOKEN=") || strings.HasPrefix(kv, "DOCKER_") {
			t.Fatalf("anonymous pull carries a credential: %v", anonymous.Env)
		}
	}
}

func TestRunTrivyRedactsTheToken(t *testing.T) {
	token := fakeJWT("signature-of-the-scanner-token")
	signature := token[strings.LastIndexByte(token, '.')+1:]
	ctx := context.Background()

	_, err := runTrivyWithCmd(fakeCommand(ctx, "unauthorized", "TRIVY_REGISTRY_TOKEN="+token), 1<<20, token)
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
