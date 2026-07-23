// Package scan runs vulnerability scans against images already in Dockyard by
// shelling out to the `trivy` CLI bundled in Dockyard's own image. Standalone
// mode (default) has trivy manage its own vulnerability DB, cached under
// TrivyCacheDir; an operator can instead point TrivyServerURL at a shared
// `trivy server --listen` process (mutualized DB across instances, or an
// air-gapped setup where Dockyard itself has no internet egress). Either way
// Dockyard's own registry endpoint is what trivy pulls the image from. This
// keeps the Go binary free of any trivy dependency (no CGO, stays
// scratch-buildable).
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

// buildTrivyArgs constructs the `trivy image` argument list. server is
// optional — empty means standalone mode (trivy manages its own DB under
// cacheDir); non-empty adds --server so trivy defers to an external
// `trivy server`. --cache-dir is always set: the final image runs FROM
// scratch with no HOME, so trivy's default cache-dir resolution is
// unreliable even in server mode (it still pulls and unpacks image layers
// locally to scan them, regardless of where the vulnerability DB lives).
func buildTrivyArgs(server, cacheDir, imageRef string, insecure bool) []string {
	args := []string{
		"image",
		"--cache-dir", cacheDir,
		"--format", "json",
		"--scanners", "vuln",
		"--quiet",
	}
	if server != "" {
		args = append(args, "--server", server)
	}
	if insecure {
		args = append(args, "--insecure")
	}
	return append(args, imageRef)
}

// runTrivy shells out to `trivy image` (standalone or --server, depending on
// whether server is set) and returns the raw JSON report. user/pass
// authenticate trivy's own registry pull against Dockyard.
func runTrivy(ctx context.Context, bin, server, cacheDir, imageRef, user, pass string, insecure bool, maxBytes int64) ([]byte, error) {
	args := buildTrivyArgs(server, cacheDir, imageRef, insecure)
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // bin/server/imageRef are operator/config-controlled, not end-user input
	// TMPDIR: the final image runs FROM scratch, which has no /tmp — trivy's
	// own DB download and layer extraction need a writable temp dir, so point
	// it at cacheDir (already created by the server at startup).
	cmd.Env = append(cmd.Environ(), "TMPDIR="+cacheDir)
	if user != "" {
		cmd.Env = append(cmd.Env, "DOCKER_USERNAME="+user, "DOCKER_PASSWORD="+pass)
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
