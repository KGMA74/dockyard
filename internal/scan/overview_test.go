package scan

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
)

// fakeResolver knows which name@digest still exist in the registry.
type fakeResolver map[string]bool

func (f fakeResolver) GetManifest(name, reference string) ([]byte, string, error) {
	if f[name+"@"+reference] {
		return []byte("{}"), reference, nil
	}
	return nil, "", errors.New("manifest unknown")
}

func digestOf(c string) string { return "sha256:" + strings.Repeat(c, 64) }

func finishedScan(t *testing.T, st *store.Store, name, ref, digest string, vulns map[string]string) int64 {
	t.Helper()
	sc, err := st.EnqueueScan(name, ref, digest, "admin")
	if err != nil {
		t.Fatal(err)
	}
	var list []store.ScanVulnerability
	counts := map[string]int{}
	for id, sev := range vulns {
		list = append(list, store.ScanVulnerability{ID: id, Severity: sev})
		counts[sev]++
	}
	if err := st.RecordScanVulnerabilities(sc.ID, list); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkScanSucceeded(sc.ID, counts["CRITICAL"], counts["HIGH"], counts["MEDIUM"], counts["LOW"], counts["UNKNOWN"], nil, "0.56.2"); err != nil {
		t.Fatal(err)
	}
	return sc.ID
}

func TestVulnerabilityOverview(t *testing.T) {
	st := openTestStore(t)
	a := finishedScan(t, st, "team/api", "1.0", digestOf("a"), map[string]string{"CVE-2024-0001": "CRITICAL", "CVE-2024-0002": "HIGH"})
	// team/web: an old scan found CVE-0001, the latest only a LOW one.
	finishedScan(t, st, "team/web", "2.0", digestOf("b"), map[string]string{"CVE-2024-0001": "CRITICAL"})
	b := finishedScan(t, st, "team/web", "2.0", digestOf("b"), map[string]string{"CVE-2024-0003": "LOW"})
	// team/old: deleted from the registry since its scan.
	finishedScan(t, st, "team/old", "0.1", digestOf("c"), map[string]string{"CVE-2024-0001": "CRITICAL"})
	if err := st.AllowCVE("CVE-2024-0002", "not reachable", "admin", nil); err != nil {
		t.Fatal(err)
	}
	policy, err := NewPolicyState(st)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.Update(false, "CRITICAL", "admin"); err != nil {
		t.Fatal(err)
	}
	resolver := fakeResolver{"team/api@" + digestOf("a"): true, "team/web@" + digestOf("b"): true}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/vulnerabilities", nil)
	rec := httptest.NewRecorder()
	if err := NewOverviewHandler(st, policy, resolver).Get(echo.New().NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var got struct {
		Images   []VulnImage `json:"images"`
		CVEs     []VulnCVE   `json:"cves"`
		CVECount int         `json:"cve_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	if len(got.Images) != 2 || got.Images[0].ScanID != a || got.Images[1].ScanID != b {
		t.Fatalf("images = %+v, want team/api (critical) then team/web, deleted team/old left out", got.Images)
	}
	if !got.Images[0].PullBlocked || got.Images[1].PullBlocked {
		t.Errorf("pull_blocked = %v / %v, want the critical image blocked only", got.Images[0].PullBlocked, got.Images[1].PullBlocked)
	}

	cves := map[string]VulnCVE{}
	for _, c := range got.CVEs {
		cves[c.ID] = c
	}
	if c := cves["CVE-2024-0001"]; len(c.ScanIDs) != 1 || c.ScanIDs[0] != a || c.Severity != "CRITICAL" {
		t.Errorf("CVE-2024-0001 = %+v, want team/api alone (web's latest scan no longer has it, old is deleted)", c)
	}
	if !cves["CVE-2024-0002"].Allowlisted || cves["CVE-2024-0001"].Allowlisted {
		t.Error("allowlist flags")
	}
	if c := cves["CVE-2024-0003"]; len(c.ScanIDs) != 1 || c.ScanIDs[0] != b {
		t.Errorf("CVE-2024-0003 = %+v", c)
	}
	if got.CVECount != 3 || got.CVEs[0].ID != "CVE-2024-0001" {
		t.Errorf("cve_count = %d, first = %s; want 3, most severe first", got.CVECount, got.CVEs[0].ID)
	}
}
