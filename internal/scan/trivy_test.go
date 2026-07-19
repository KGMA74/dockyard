package scan

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestMain lets this test binary re-exec itself as a fake `trivy` process,
// the same technique the Go standard library uses to test os/exec callers
// without depending on a real external binary (see exec_test.go upstream).
func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		os.Exit(m.Run())
	}
	runHelperProcess()
	os.Exit(0)
}

// runHelperProcess emulates `trivy` based on env vars set by the test that
// launched this re-exec'd process.
func runHelperProcess() {
	switch os.Getenv("HELPER_MODE") {
	case "version":
		fmt.Println("Version: 0.56.2-test")
	case "report":
		fmt.Print(os.Getenv("HELPER_REPORT"))
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
	got, err := runTrivyWithCmd(fakeCommand(ctx, "report", "HELPER_REPORT="+report), 1<<20)
	if err != nil {
		t.Fatalf("runTrivyWithCmd: %v", err)
	}
	if string(got) != report {
		t.Fatalf("runTrivyWithCmd = %q, want %q", got, report)
	}
}

func TestRunTrivyFailure(t *testing.T) {
	ctx := context.Background()
	_, err := runTrivyWithCmd(fakeCommand(ctx, "fail"), 1<<20)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unable to pull image") {
		t.Fatalf("err = %v, want stderr message included", err)
	}
}

func TestRunTrivyOversized(t *testing.T) {
	ctx := context.Background()
	_, err := runTrivyWithCmd(fakeCommand(ctx, "huge"), 1024) // helper emits 10KB
	if err != errReportTooLarge {
		t.Fatalf("err = %v, want errReportTooLarge", err)
	}
}

func TestRunTrivyTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := runTrivyWithCmd(fakeCommand(ctx, "hang"), 1<<20)
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
