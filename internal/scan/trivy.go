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
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"dockyard/internal/store"
)

// trivyReport is the minimal shape of `trivy image --format json` output
// needed to tally vulnerabilities by severity. Pin against the trivy version
// bundled in the Dockerfile — a trivy upgrade may require adjusting this.
type trivyReport struct {
	Results []struct {
		Vulnerabilities []struct {
			VulnerabilityID string `json:"VulnerabilityID"`
			Severity        string `json:"Severity"`
		} `json:"Vulnerabilities"`
	} `json:"Results"`
}

var severityRank = map[string]int{"CRITICAL": 4, "HIGH": 3, "MEDIUM": 2, "LOW": 1}

// distinctVulnerabilities lists each vulnerability once — a CVE found in three
// packages is one thing to fix or to accept — at its highest severity.
func distinctVulnerabilities(report []byte) ([]store.ScanVulnerability, error) {
	var r trivyReport
	if err := json.Unmarshal(report, &r); err != nil {
		return nil, fmt.Errorf("scan: parse trivy report: %w", err)
	}
	worst := map[string]string{}
	for _, res := range r.Results {
		for _, v := range res.Vulnerabilities {
			if v.VulnerabilityID == "" {
				continue
			}
			sev := strings.ToUpper(v.Severity)
			if _, known := severityRank[sev]; !known {
				sev = "UNKNOWN"
			}
			if prev, seen := worst[v.VulnerabilityID]; !seen || severityRank[sev] > severityRank[prev] {
				worst[v.VulnerabilityID] = sev
			}
		}
	}
	out := make([]store.ScanVulnerability, 0, len(worst))
	for id, sev := range worst {
		out = append(out, store.ScanVulnerability{ID: id, Severity: sev})
	}
	return out, nil
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
func buildTrivyArgs(server, cacheDir, imageRef string, insecure, offline bool, timeout time.Duration) []string {
	args := []string{
		"image",
		"--cache-dir", cacheDir,
		"--format", "json",
		"--scanners", "vuln",
		"--quiet",
	}
	if timeout > 0 {
		// Trivy has its own internal timeout (default 5m); align it with ours so
		// a slow pull/extract fails with a trivy error rather than a bare
		// context cancellation.
		args = append(args, "--timeout", timeout.String())
	}
	if offline {
		// Scan with only the seeded/cached DB — never touch the network.
		args = append(args, "--skip-db-update", "--skip-java-db-update")
	}
	if server != "" {
		args = append(args, "--server", server)
	}
	if insecure {
		args = append(args, "--insecure")
	}
	return append(args, imageRef)
}

// trivyRun is one `trivy image` invocation.
type trivyRun struct {
	bin, server, cacheDir, imageRef string
	insecure, offline               bool
	timeout                         time.Duration
	maxBytes                        int64
	// registryToken is the bearer token trivy pulls the image with; empty
	// means an anonymous pull. It only ever travels in the child's
	// environment as TRIVY_REGISTRY_TOKEN: never on the command line (ps
	// shows it to every local user), in a log, an error or a stored report.
	registryToken string
}

// command builds the trivy process. Trivy 0.56 reads registry credentials
// from TRIVY_USERNAME/TRIVY_PASSWORD or TRIVY_REGISTRY_TOKEN only. The token
// is the one that fits Dockyard: trivy sends it as-is as a Bearer header on
// /v2/* without going through /v2/token, where it would need a password.
func (r trivyRun) command(ctx context.Context) *exec.Cmd {
	args := buildTrivyArgs(r.server, r.cacheDir, r.imageRef, r.insecure, r.offline, r.timeout)
	cmd := exec.CommandContext(ctx, r.bin, args...) //nolint:gosec // bin/server/imageRef are operator/config-controlled, not end-user input
	var extra []string
	if r.registryToken != "" {
		extra = append(extra, "TRIVY_REGISTRY_TOKEN="+r.registryToken)
	}
	cmd.Env = trivyEnv(r.cacheDir, extra...)
	return cmd
}

// runTrivy shells out to `trivy image` (standalone or --server, depending on
// whether server is set) and returns the raw JSON report.
func runTrivy(ctx context.Context, r trivyRun) ([]byte, error) {
	return runTrivyWithCmd(r.command(ctx), r.maxBytes, r.registryToken)
}

// trivyPassEnv lists the only variables trivy inherits from Dockyard's
// environment: what a process needs to run, and to reach the network (DB
// download, an external trivy server) through a proxy or with a private CA.
// Everything else stays out: Dockyard's environment holds JWT_SECRET, the
// admin password, storage and upstream registry credentials. Names are
// matched case-insensitively (http_proxy, and Windows' Path/SystemRoot).
var trivyPassEnv = []string{
	"PATH",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
	"SSL_CERT_FILE", "SSL_CERT_DIR",
	"SYSTEMROOT", // Windows: needed by the network and crypto stacks
}

// trivyEnv builds the explicit environment of a trivy process: the
// pass-through list above, TMPDIR and HOME pointed at cacheDir, then extra.
// TMPDIR: the final image runs FROM scratch, which has no /tmp — trivy's
// DB download and layer extraction need a writable temp dir, and cacheDir is
// created by the server at startup. HOME likewise, so nothing trivy derives
// from it lands outside the cache.
func trivyEnv(cacheDir string, extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			continue
		}
		if slices.ContainsFunc(trivyPassEnv, func(name string) bool { return strings.EqualFold(name, key) }) {
			env = append(env, kv)
		}
	}
	env = append(env, "TMPDIR="+cacheDir)
	if cacheDir != "" {
		env = append(env, "HOME="+cacheDir)
	}
	return append(env, extra...)
}

const redacted = "[REDACTED]"

// redact removes secret from s, as a whole and its signature alone (the
// part of a JWT that makes it usable), in case trivy echoes either back.
func redact(s []byte, secret string) []byte {
	if secret == "" {
		return s
	}
	s = bytes.ReplaceAll(s, []byte(secret), []byte(redacted))
	if i := strings.LastIndexByte(secret, '.'); i >= 0 && len(secret)-i > 16 {
		s = bytes.ReplaceAll(s, []byte(secret[i+1:]), []byte(redacted))
	}
	return s
}

// runTrivyWithCmd runs a pre-built trivy command and returns its stdout,
// capped at maxBytes. Split out from runTrivy so tests can substitute a fake
// binary without touching the real command construction. secret is the
// registry token the command carries, if any: it is scrubbed from both the
// error and the report before either leaves this function.
func runTrivyWithCmd(cmd *exec.Cmd, maxBytes int64, secret string) ([]byte, error) {
	stdout := &cappedBuffer{maxBytes: maxBytes}
	var stderr bytes.Buffer
	cmd.Stdout = stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(string(redact(stderr.Bytes(), secret)))
		if msg == "" {
			msg = string(redact([]byte(err.Error()), secret))
		}
		return nil, fmt.Errorf("scan: trivy failed: %s%s", msg, trivyErrorHint(msg, secret != ""))
	}
	if stdout.exceeded {
		return nil, errReportTooLarge
	}
	return redact(stdout.buf.Bytes(), secret), nil
}

// downloadTrivyDB refreshes the vulnerability DB in the given cache dir. Used
// for a best-effort background warm-up on startup so the DB shipped in the image
// (which is as old as the build) is current before the first user scan.
func downloadTrivyDB(ctx context.Context, bin, cacheDir string) error {
	cmd := exec.CommandContext(ctx, bin, "--cache-dir", cacheDir, "image", "--download-db-only") //nolint:gosec // bin is operator/config-controlled
	cmd.Env = trivyEnv(cacheDir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("scan: trivy db refresh: %s", msg)
	}
	return nil
}

// trivyErrorHint maps common trivy failures to an actionable suggestion,
// appended to the raw error the operator sees. withToken tells whether trivy
// was given a registry token for its pull.
func trivyErrorHint(stderr string, withToken bool) string {
	s := strings.ToLower(stderr)
	switch {
	case strings.Contains(s, "vulnerability db") && (strings.Contains(s, "download") || strings.Contains(s, "update")):
		return " (hint: no internet access to fetch the vulnerability DB — set TRIVY_OFFLINE=true to use only the DB shipped in the image, or point TRIVY_SERVER_URL at a shared trivy server)"
	case strings.Contains(s, "deadline exceeded") || strings.Contains(s, "timeout"):
		return " (hint: the scan timed out — raise SCAN_TIMEOUT; the first scan is slower while the DB is prepared)"
	case strings.Contains(s, "401") || strings.Contains(s, "unauthorized"):
		if withToken {
			return " (hint: the registry refused the short-lived pull token Dockyard gave the scanner for this repository (401) — trivy pulls from this Dockyard instance through localhost on its own PORT; if another service answers on that port, the token never reaches Dockyard)"
		}
		return " (hint: trivy pulled without credentials and the registry answered 401 — Dockyard gives the scanner a pull token only in embedded mode with V2_AUTH_ENABLED=true; in proxy mode the pull goes through Dockyard's registry authentication with none)"
	case strings.Contains(s, "manifest_unknown") || strings.Contains(s, "manifest unknown"):
		return " (hint: trivy could not find the image in Dockyard — check it still exists, it may have been deleted or garbage-collected since the scan was queued)"
	case strings.Contains(s, "403 forbidden"):
		return " (hint: the registry refused trivy's pull (403) — the scanner's token covers only the repository being scanned, read-only)"
	default:
		return ""
	}
}

// trivyVersion runs `trivy --version` once and extracts the version string,
// e.g. "Version: 0.56.2" -> "0.56.2".
func trivyVersion(ctx context.Context, bin, cacheDir string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, "--version") //nolint:gosec // bin is operator/config-controlled
	cmd.Env = trivyEnv(cacheDir)
	return trivyVersionWithCmd(cmd)
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
