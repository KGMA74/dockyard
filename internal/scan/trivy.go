// Package scan runs vulnerability scans against images already in Dockyard by
// shelling out to the `trivy` CLI in `--server` mode: an operator-managed
// `trivy server --listen` process hosts the vulnerability DB, and Dockyard's
// own registry endpoint is what trivy pulls the image from. This keeps the Go
// binary free of any trivy dependency (no CGO, stays scratch-buildable).
package scan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// trivyReport is the minimal shape of `trivy image --format json` output
// needed to tally vulnerabilities by severity. Pin against the trivy version
// bundled in the Dockerfile — a trivy upgrade may require adjusting this.
type trivyReport struct {
	Results []struct {
		Vulnerabilities []struct {
			Severity string `json:"Severity"`
		} `json:"Vulnerabilities"`
	} `json:"Results"`
}

// severityCounts tallies a trivy JSON report by severity.
type severityCounts struct {
	Critical, High, Medium, Low, Unknown int
}

func tallySeverities(report []byte) (severityCounts, error) {
	var r trivyReport
	if err := json.Unmarshal(report, &r); err != nil {
		return severityCounts{}, fmt.Errorf("scan: parse trivy report: %w", err)
	}
	var c severityCounts
	for _, res := range r.Results {
		for _, v := range res.Vulnerabilities {
			switch strings.ToUpper(v.Severity) {
			case "CRITICAL":
				c.Critical++
			case "HIGH":
				c.High++
			case "MEDIUM":
				c.Medium++
			case "LOW":
				c.Low++
			default:
				c.Unknown++
			}
		}
	}
	return c, nil
}

// errReportTooLarge is returned by runTrivy when the report output exceeds
// maxBytes, so the caller reports a clear failure instead of storing
// truncated (invalid) JSON.
var errReportTooLarge = fmt.Errorf("scan: trivy report exceeded the configured size limit")

// cappedBuffer stops accepting writes once maxBytes is exceeded, so a
// runaway trivy process can't grow stdout unbounded in memory.
type cappedBuffer struct {
	buf      bytes.Buffer
	maxBytes int64
	exceeded bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.exceeded {
		return len(p), nil // drain silently, cmd.Run() still needs stdout consumed
	}
	if int64(c.buf.Len()+len(p)) > c.maxBytes {
		c.exceeded = true
		return len(p), nil
	}
	return c.buf.Write(p)
}

// runTrivy shells out to `trivy image --server ...` and returns the raw JSON
// report. user/pass authenticate trivy's own registry pull against Dockyard.
func runTrivy(ctx context.Context, bin, server, imageRef, user, pass string, insecure bool, maxBytes int64) ([]byte, error) {
	args := []string{
		"image",
		"--server", server,
		"--format", "json",
		"--scanners", "vuln",
		"--quiet",
	}
	if insecure {
		args = append(args, "--insecure")
	}
	args = append(args, imageRef)
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // bin/server/imageRef are operator/config-controlled, not end-user input
	if user != "" {
		cmd.Env = append(cmd.Environ(), "DOCKER_USERNAME="+user, "DOCKER_PASSWORD="+pass)
	}
	return runTrivyWithCmd(cmd, maxBytes)
}

// runTrivyWithCmd runs a pre-built trivy command and returns its stdout,
// capped at maxBytes. Split out from runTrivy so tests can substitute a fake
// binary without touching the real command construction.
func runTrivyWithCmd(cmd *exec.Cmd, maxBytes int64) ([]byte, error) {
	stdout := &cappedBuffer{maxBytes: maxBytes}
	var stderr bytes.Buffer
	cmd.Stdout = stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("scan: trivy failed: %s", msg)
	}
	if stdout.exceeded {
		return nil, errReportTooLarge
	}
	return stdout.buf.Bytes(), nil
}

// trivyVersion runs `trivy --version` once and extracts the version string,
// e.g. "Version: 0.56.2" -> "0.56.2".
func trivyVersion(ctx context.Context, bin string) (string, error) {
	return trivyVersionWithCmd(exec.CommandContext(ctx, bin, "--version")) //nolint:gosec // bin is operator/config-controlled
}

func trivyVersionWithCmd(cmd *exec.Cmd) (string, error) {
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("scan: trivy --version failed: %w", err)
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if v, ok := strings.CutPrefix(line, "Version: "); ok {
			return strings.TrimSpace(v), nil
		}
	}
	return "", nil
}
