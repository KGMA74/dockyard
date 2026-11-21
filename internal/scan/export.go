package scan

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"dockyard/internal/pdf"
	"dockyard/internal/store"
)

// Report export: the raw trivy JSON as stored, a CSV of the findings for a
// spreadsheet, or a PDF to hand to someone who will not open either.

// reportRow is one finding: a vulnerability in one package of one target,
// the same rows the console's report shows.
type reportRow struct {
	Severity    string
	ID          string
	Package     string
	Installed   string
	Fixed       string
	Target      string
	Title       string
	URL         string
	Allowlisted bool // on the CVE allowlist (accepted risk) when exported
}

type trivyFullReport struct {
	Results []struct {
		Target          string `json:"Target"`
		Vulnerabilities []struct {
			VulnerabilityID  string `json:"VulnerabilityID"`
			PkgName          string `json:"PkgName"`
			InstalledVersion string `json:"InstalledVersion"`
			FixedVersion     string `json:"FixedVersion"`
			Severity         string `json:"Severity"`
			Title            string `json:"Title"`
			PrimaryURL       string `json:"PrimaryURL"`
		} `json:"Vulnerabilities"`
	} `json:"Results"`
}

var severityOrder = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW", "UNKNOWN"}

// reportRows flattens a trivy report, most severe first, then by package
// and ID. allowed holds the CVE IDs on the active allowlist.
func reportRows(raw []byte, allowed map[string]bool) ([]reportRow, error) {
	var r trivyFullReport
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("scan: parse trivy report: %w", err)
	}
	var rows []reportRow
	for _, res := range r.Results {
		for _, v := range res.Vulnerabilities {
			sev := strings.ToUpper(v.Severity)
			if _, known := severityRank[sev]; !known {
				sev = "UNKNOWN"
			}
			rows = append(rows, reportRow{
				Severity: sev, ID: v.VulnerabilityID, Package: v.PkgName,
				Installed: v.InstalledVersion, Fixed: v.FixedVersion,
				Target: res.Target, Title: v.Title, URL: v.PrimaryURL,
				Allowlisted: allowed[strings.ToUpper(v.VulnerabilityID)],
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if severityRank[a.Severity] != severityRank[b.Severity] {
			return severityRank[a.Severity] > severityRank[b.Severity]
		}
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		return a.ID < b.ID
	})
	return rows, nil
}

// activeAllowlist returns the CVE IDs whose allowlist entry has not expired,
// the same rule the pull gate applies.
func activeAllowlist(entries []*store.AllowedCVE, now time.Time) map[string]bool {
	cutoff := now.UTC().Format(time.RFC3339)
	out := map[string]bool{}
	for _, e := range entries {
		if e.ExpiresAt == nil || *e.ExpiresAt > cutoff {
			out[strings.ToUpper(e.CVEID)] = true
		}
	}
	return out
}

const utf8BOM = "\xef\xbb\xbf"

// writeCSV writes the findings as CSV, UTF-8 with a byte order mark so a
// spreadsheet opened by double-click reads accents right.
func writeCSV(w io.Writer, rows []reportRow) error {
	if _, err := io.WriteString(w, utf8BOM); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"severity", "vulnerability_id", "package", "installed_version", "fixed_version", "target", "title", "url", "allowlisted"})
	for _, r := range rows {
		_ = cw.Write([]string{
			r.Severity, csvCell(r.ID), csvCell(r.Package), csvCell(r.Installed), csvCell(r.Fixed),
			csvCell(r.Target), csvCell(r.Title), csvCell(r.URL), strconv.FormatBool(r.Allowlisted),
		})
	}
	cw.Flush()
	return cw.Error()
}

// csvCell neutralizes formula injection: report text comes from vulnerability
// databases and image contents, and a cell starting with = + - @ would run as
// a formula in a spreadsheet.
func csvCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

var unsafeFilename = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// reportFilename names the download after the image, e.g.
// dockyard-scan-team-app-1.0.pdf, or the digest's first 12 hex digits when
// the scan was asked for by digest.
func reportFilename(sc *store.ScanResult, ext string) string {
	ref := sc.Reference
	if strings.HasPrefix(ref, "sha256:") || ref == "" {
		ref = strings.TrimPrefix(sc.Digest, "sha256:")
		if len(ref) > 12 {
			ref = ref[:12]
		}
	}
	base := unsafeFilename.ReplaceAllString(strings.ReplaceAll(sc.Name, "/", "-")+"-"+ref, "_")
	return "dockyard-scan-" + strings.Trim(base, "_") + "." + ext
}

// ── PDF ─────────────────────────────────────────────────────────────────────

var pdfStrings = map[string]map[string]string{
	"en": {
		"title": "Vulnerability report", "image": "Image", "digest": "Digest",
		"scanned": "Scanned", "scanner": "Scanner", "generated": "Generated",
		"severity": "Severity", "id": "Vulnerability", "package": "Package",
		"installed": "Installed", "fixed": "Fixed in", "summary": "Title",
		"none": "No vulnerabilities found.", "allowlisted": "* on the CVE allowlist (accepted risk) when this report was generated",
		"findings": "%d findings", "page": "Page %d of %d", "noFix": "—",
	},
	"fr": {
		"title": "Rapport de vulnérabilités", "image": "Image", "digest": "Digest",
		"scanned": "Scanné le", "scanner": "Scanner", "generated": "Généré le",
		"severity": "Sévérité", "id": "Vulnérabilité", "package": "Paquet",
		"installed": "Installée", "fixed": "Corrigée dans", "summary": "Titre",
		"none": "Aucune vulnérabilité trouvée.", "allowlisted": "* dans la liste d'exceptions CVE (risque accepté) à la génération du rapport",
		"findings": "%d résultats", "page": "Page %d sur %d", "noFix": "—",
		// Same words as the console.
		"CRITICAL": "CRITIQUE", "HIGH": "ÉLEVÉE", "MEDIUM": "MOYENNE", "LOW": "FAIBLE", "UNKNOWN": "INCONNUE",
	},
}

// severityLabel is the severity as printed in the PDF; the CSV keeps trivy's
// values, which tooling filters on.
func severityLabel(s map[string]string, sev string) string {
	if label, ok := s[sev]; ok {
		return label
	}
	return sev
}

var (
	ink      = pdf.Color{R: 0.09, G: 0.09, B: 0.11}
	muted    = pdf.Color{R: 0.42, G: 0.42, B: 0.46}
	rule     = pdf.Color{R: 0.85, G: 0.85, B: 0.87}
	stripe   = pdf.Color{R: 0.965, G: 0.965, B: 0.97}
	white    = pdf.Color{R: 1, G: 1, B: 1}
	sevColor = map[string]pdf.Color{
		"CRITICAL": {R: 0.70, G: 0.11, B: 0.11},
		"HIGH":     {R: 0.85, G: 0.33, B: 0.10},
		"MEDIUM":   {R: 0.80, G: 0.55, B: 0.02},
		"LOW":      {R: 0.20, G: 0.45, B: 0.75},
		"UNKNOWN":  {R: 0.50, G: 0.50, B: 0.53},
	}
)

// Page geometry: A4 landscape, points.
const (
	pageW, pageH = 842.0, 595.0
	margin       = 36.0
	rowH         = 15.0
	cellSize     = 7.5 // Courier, table cells
	headSize     = 7.5 // Helvetica-Bold, table header
	firstTableY  = 196.0
	nextTableY   = 64.0
	bottomY      = pageH - 48
)

type column struct {
	key   string
	width float64
}

var columns = []column{
	// Package is wide enough for Maven coordinates (group:artifact).
	{"severity", 62}, {"id", 112}, {"package", 205}, {"installed", 82}, {"fixed", 82}, {"summary", 0},
}

// renderPDF lays the report out: a header with the image and the counts on
// the first page, then the findings table, its header repeated on every page.
func renderPDF(sc *store.ScanResult, rows []reportRow, lang string, now time.Time) ([]byte, error) {
	s, ok := pdfStrings[lang]
	if !ok {
		s = pdfStrings["en"]
	}
	image := sc.Name + ":" + sc.Reference
	if strings.HasPrefix(sc.Reference, "sha256:") {
		image = sc.Name + "@" + sc.Reference
	}
	doc := pdf.New(pageW, pageH, s["title"]+" — "+image, now)

	cols := append([]column(nil), columns...)
	used := 0.0
	for _, c := range cols[:len(cols)-1] {
		used += c.width
	}
	cols[len(cols)-1].width = pageW - 2*margin - used

	perFirst := rowsBetween(firstTableY, bottomY)
	perNext := rowsBetween(nextTableY, bottomY)
	pages := 1
	if len(rows) > perFirst {
		pages += (len(rows) - perFirst + perNext - 1) / perNext
	}

	anyAllowlisted := false
	for _, r := range rows {
		anyAllowlisted = anyAllowlisted || r.Allowlisted
	}

	next := 0
	for n := 1; n <= pages; n++ {
		p := doc.AddPage()
		y := nextTableY
		if n == 1 {
			drawHeader(p, sc, rows, s, image, now)
			y = firstTableY
		}
		if len(rows) == 0 {
			p.Text(margin, y+12, pdf.Helvetica, 11, muted, s["none"])
		} else {
			y = drawTableHeader(p, cols, s, y)
			limit := perNext
			if n == 1 {
				limit = perFirst
			}
			for i := 0; i < limit && next < len(rows); i++ {
				drawRow(p, cols, rows[next], y, next%2 == 1, s)
				y += rowH
				next++
			}
		}
		// Footer: the allowlist footnote when it applies, else the image;
		// the page number on the right.
		p.Line(margin, pageH-34, pageW-margin, pageH-34, 0.5, rule)
		if anyAllowlisted {
			p.Text(margin, pageH-22, pdf.Helvetica, 7, muted, s["allowlisted"])
		} else {
			p.Text(margin, pageH-22, pdf.Courier, 7, muted, pdf.MonoFit("Dockyard · "+image, 7, pageW-2*margin-120))
		}
		pageNo := fmt.Sprintf(s["page"], n, pages)
		p.Text(pageW-margin-pdf.MonoWidth(7, len([]rune(pageNo))), pageH-22, pdf.Courier, 7, muted, pageNo)
	}
	return doc.Bytes()
}

// rowsBetween is how many table rows fit from top to bottom, below the
// column headings.
func rowsBetween(top, bottom float64) int {
	return int((bottom - top - rowH) / rowH)
}

func drawHeader(p *pdf.Page, sc *store.ScanResult, rows []reportRow, s map[string]string, image string, now time.Time) {
	p.Text(margin, 40, pdf.HelveticaBold, 9, muted, "DOCKYARD")
	p.Text(margin, 64, pdf.HelveticaBold, 18, ink, s["title"])

	scanned := ""
	if sc.FinishedAt != nil {
		if t, ok := parseTime(sc.FinishedAt); ok {
			scanned = t.UTC().Format("2006-01-02 15:04 UTC")
		}
	}
	scanner := "Trivy"
	if sc.TrivyVersion != "" {
		scanner += " " + sc.TrivyVersion
	}
	lines := [][2]string{
		{s["image"], image},
		{s["digest"], sc.Digest},
		{s["scanned"], scanned},
		{s["scanner"], scanner},
		{s["generated"], now.UTC().Format("2006-01-02 15:04 UTC")},
	}
	y := 88.0
	for _, l := range lines {
		p.Text(margin, y, pdf.Helvetica, 8.5, muted, l[0])
		p.Text(margin+70, y, pdf.Courier, 8.5, ink, pdf.MonoFit(l[1], 8.5, pageW-2*margin-70))
		y += 13
	}

	// Severity counts, from the rows so they always match the table.
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Severity]++
	}
	x := margin
	y = 160
	for _, sev := range severityOrder {
		label := fmt.Sprintf("%s %d", severityLabel(s, sev), counts[sev])
		w := pdf.MonoWidth(8, len(label)) + 14
		c := sevColor[sev]
		if counts[sev] == 0 {
			c = rule
		}
		p.Rect(x, y-11, w, 16, c)
		text := white
		if counts[sev] == 0 {
			text = muted
		}
		p.Text(x+7, y, pdf.CourierBold, 8, text, label)
		x += w + 6
	}
	p.Text(x+6, y, pdf.Helvetica, 8.5, muted, fmt.Sprintf(s["findings"], len(rows)))
}

func drawTableHeader(p *pdf.Page, cols []column, s map[string]string, y float64) float64 {
	x := margin
	for _, c := range cols {
		p.Text(x+4, y+10, pdf.HelveticaBold, headSize, muted, strings.ToUpper(s[c.key]))
		x += c.width
	}
	p.Line(margin, y+rowH, pageW-margin, y+rowH, 0.75, rule)
	return y + rowH
}

func drawRow(p *pdf.Page, cols []column, r reportRow, y float64, striped bool, s map[string]string) {
	if striped {
		p.Rect(margin, y, pageW-2*margin, rowH, stripe)
	}
	id := r.ID
	if r.Allowlisted {
		id += " *"
	}
	fixed := r.Fixed
	if fixed == "" {
		fixed = s["noFix"]
	}
	values := map[string]string{
		"severity": severityLabel(s, r.Severity), "id": id, "package": r.Package,
		"installed": r.Installed, "fixed": fixed, "summary": r.Title,
	}
	x := margin
	for _, c := range cols {
		font, color := pdf.Courier, ink
		if c.key == "severity" {
			font, color = pdf.CourierBold, sevColor[r.Severity]
		}
		if c.key == "fixed" && r.Fixed == "" {
			color = muted
		}
		p.Text(x+4, y+10.5, font, cellSize, color, pdf.MonoFit(values[c.key], cellSize, c.width-8))
		x += c.width
	}
}
