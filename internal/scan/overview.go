package scan

import (
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
)

// The registry-wide vulnerability view: every image's current scan ranked by
// risk, and the vulnerabilities that affect the most images.

// VulnImage is one scanned image and its latest successful scan.
type VulnImage struct {
	ScanID       int64   `json:"scan_id"`
	Name         string  `json:"name"`
	Reference    string  `json:"reference"`
	Digest       string  `json:"digest"`
	TrivyVersion string  `json:"trivy_version,omitempty"`
	Critical     int     `json:"critical_count"`
	High         int     `json:"high_count"`
	Medium       int     `json:"medium_count"`
	Low          int     `json:"low_count"`
	Unknown      int     `json:"unknown_count"`
	FinishedAt   *string `json:"finished_at,omitempty"`
	PullBlocked  bool    `json:"pull_blocked,omitempty"`
}

// VulnCVE is one vulnerability and the images (by scan) it affects.
type VulnCVE struct {
	ID          string  `json:"id"`
	Severity    string  `json:"severity"`
	ScanIDs     []int64 `json:"scan_ids"`
	Allowlisted bool    `json:"allowlisted,omitempty"`
}

// maxOverviewCVEs bounds the CVE list; the most widespread and severe come
// first, and the rest stay one click away in each image's report.
const maxOverviewCVEs = 500

// existsConcurrency bounds the manifest lookups that drop deleted images.
const existsConcurrency = 8

// OverviewHandler serves GET /api/admin/vulnerabilities.
type OverviewHandler struct {
	store    *store.Store
	policy   *PolicyState
	resolver manifestResolver
}

func NewOverviewHandler(st *store.Store, policy *PolicyState, resolver manifestResolver) *OverviewHandler {
	return &OverviewHandler{store: st, policy: policy, resolver: resolver}
}

// Get — GET /api/admin/vulnerabilities (admin, scanning on). Images deleted
// since their scan are left out; a CVE counts the remaining images only.
func (h *OverviewHandler) Get(c echo.Context) error {
	scans, err := h.store.LatestSuccessfulScans()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	scans = h.stillPresent(scans)

	images := make([]VulnImage, 0, len(scans))
	kept := make(map[int64]bool, len(scans))
	for _, sc := range scans {
		kept[sc.ID] = true
		images = append(images, VulnImage{
			ScanID: sc.ID, Name: sc.Name, Reference: sc.Reference, Digest: sc.Digest, TrivyVersion: sc.TrivyVersion,
			Critical: sc.CriticalCount, High: sc.HighCount, Medium: sc.MediumCount, Low: sc.LowCount, Unknown: sc.UnknownCount,
			FinishedAt: sc.FinishedAt, PullBlocked: h.policy != nil && h.policy.Blocks(sc.Digest),
		})
	}
	sort.SliceStable(images, func(i, j int) bool {
		a, b := images[i], images[j]
		for _, d := range [][2]int{{a.Critical, b.Critical}, {a.High, b.High}, {a.Medium, b.Medium}, {a.Low, b.Low}} {
			if d[0] != d[1] {
				return d[0] > d[1]
			}
		}
		return a.Name+":"+a.Reference < b.Name+":"+b.Reference
	})

	findings, err := h.store.FindingsOfLatestScans()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	entries, err := h.store.ListAllowedCVEs()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	allowed := activeAllowlist(entries, time.Now())
	byID := map[string]*VulnCVE{}
	for _, f := range findings {
		if !kept[f.ScanID] {
			continue
		}
		id := strings.ToUpper(f.VulnID)
		cve := byID[id]
		if cve == nil {
			cve = &VulnCVE{ID: f.VulnID, Severity: f.Severity, Allowlisted: allowed[id]}
			byID[id] = cve
		}
		if severityRank[f.Severity] > severityRank[cve.Severity] {
			cve.Severity = f.Severity
		}
		cve.ScanIDs = append(cve.ScanIDs, f.ScanID)
	}
	cves := make([]VulnCVE, 0, len(byID))
	for _, cve := range byID {
		cves = append(cves, *cve)
	}
	sort.Slice(cves, func(i, j int) bool {
		a, b := cves[i], cves[j]
		if severityRank[a.Severity] != severityRank[b.Severity] {
			return severityRank[a.Severity] > severityRank[b.Severity]
		}
		if len(a.ScanIDs) != len(b.ScanIDs) {
			return len(a.ScanIDs) > len(b.ScanIDs)
		}
		return a.ID < b.ID
	})
	total := len(cves)
	if len(cves) > maxOverviewCVEs {
		cves = cves[:maxOverviewCVEs]
	}
	return c.JSON(http.StatusOK, map[string]any{"images": images, "cves": cves, "cve_count": total})
}

// stillPresent drops the scans of images deleted since: a scan outlives its
// image in the database, but a list of what to fix should not.
func (h *OverviewHandler) stillPresent(scans []*store.ScanResult) []*store.ScanResult {
	if h.resolver == nil {
		return scans
	}
	present := make([]bool, len(scans))
	sem := make(chan struct{}, existsConcurrency)
	var wg sync.WaitGroup
	for i, sc := range scans {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, sc *store.ScanResult) {
			defer wg.Done()
			defer func() { <-sem }()
			_, _, err := h.resolver.GetManifest(sc.Name, sc.Digest)
			present[i] = err == nil
		}(i, sc)
	}
	wg.Wait()
	out := scans[:0]
	for i, sc := range scans {
		if present[i] {
			out = append(out, sc)
		}
	}
	return out
}
