package scan

import (
	"bytes"
	"compress/zlib"
	"encoding/csv"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"dockyard/internal/audit"
	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
)

// A report with what the exports must cope with: several severities, a CVE
// on the allowlist, one with no fix, PDF-special characters, accents, and a
// title written to run as a spreadsheet formula.
const exportTestReport = `{"Results":[
 {"Target":"app/app.jar","Vulnerabilities":[
  {"VulnerabilityID":"CVE-2021-44228","PkgName":"org.apache.logging.log4j:log4j-core","InstalledVersion":"2.14.1","FixedVersion":"2.15.0","Severity":"CRITICAL","Title":"log4j (Log4Shell): remote code execution \\ JNDI","PrimaryURL":"https://avd.aquasec.com/nvd/cve-2021-44228"},
  {"VulnerabilityID":"CVE-2024-0002","PkgName":"zlib","InstalledVersion":"1.2.13","FixedVersion":"","Severity":"low","Title":"=HYPERLINK(\"http://evil\",\"click\")"}
 ]},
 {"Target":"alpine 3.20 (os)","Vulnerabilities":[
  {"VulnerabilityID":"CVE-2024-0001","PkgName":"openssl","InstalledVersion":"3.3.1-r0","FixedVersion":"3.3.2-r0","Severity":"HIGH","Title":"openssl: dépassement de tampon"},
  {"VulnerabilityID":"GHSA-xxxx","PkgName":"busybox","InstalledVersion":"1.36","Severity":"weird","Title":"unknown severity"}
 ]}
]}`

// newExportFixture stores a finished scan of team/app:1.0 with that report,
// and puts CVE-2024-0001 on the allowlist.
func newExportFixture(t *testing.T) (*Handler, *store.ScanResult) {
	t.Helper()
	st := openTestStore(t)
	sc, err := st.EnqueueScan("team/app", "1.0", "sha256:"+strings.Repeat("ab", 32), "admin")
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzipCompress([]byte(exportTestReport))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MarkScanSucceeded(sc.ID, 1, 1, 0, 1, 1, gz, "0.56.2"); err != nil {
		t.Fatal(err)
	}
	if err := st.AllowCVE("CVE-2024-0001", "not reachable", "admin", nil); err != nil {
		t.Fatal(err)
	}
	expired := time.Now().Add(-time.Hour)
	if err := st.AllowCVE("CVE-2021-44228", "expired exception", "admin", &expired); err != nil {
		t.Fatal(err)
	}
	sc, err = st.ScanByID(sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	return NewHandler(st, nil, nil, audit.New(st)), sc
}

func getReport(t *testing.T, h *Handler, id int64, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/scans/"+itoa(id)+"/report"+query, nil)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(itoa(id))
	if err := h.Report(c); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestReportExportJSONIsTheRawReportAsAFile(t *testing.T) {
	h, sc := newExportFixture(t)
	inline := getReport(t, h, sc.ID, "")
	if inline.Body.String() != exportTestReport || inline.Header().Get("Content-Disposition") != "" {
		t.Fatalf("the console's inline read changed: %q / %q", inline.Header().Get("Content-Disposition"), inline.Body.String()[:40])
	}
	rec := getReport(t, h, sc.ID, "?format=json")
	if rec.Code != http.StatusOK || rec.Body.String() != exportTestReport {
		t.Fatalf("json export = %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="dockyard-scan-team-app-1.0.json"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
}

func TestReportExportCSV(t *testing.T) {
	h, sc := newExportFixture(t)
	rec := getReport(t, h, sc.ID, "?format=csv")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("csv export = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="dockyard-scan-team-app-1.0.csv"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, utf8BOM) {
		t.Fatal("no byte order mark: a spreadsheet would garble the accents")
	}
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, utf8BOM))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 5 || strings.Join(records[0], ",") != "severity,vulnerability_id,package,installed_version,fixed_version,target,title,url,allowlisted" {
		t.Fatalf("records = %v", records)
	}
	var order []string
	for _, r := range records[1:] {
		order = append(order, r[0]+" "+r[1]+" "+r[8])
	}
	want := []string{"CRITICAL CVE-2021-44228 false", "HIGH CVE-2024-0001 true", "LOW CVE-2024-0002 false", "UNKNOWN GHSA-xxxx false"}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Fatalf("rows = %v, want %v (most severe first; only unexpired allowlist entries count)", order, want)
	}
	if title := records[3][6]; !strings.HasPrefix(title, "'=") {
		t.Fatalf("formula not neutralized: %q", title)
	}
	if !strings.Contains(body, "dépassement") {
		t.Fatal("accents lost")
	}
}

// pdfText returns the text drawn on each page of a PDF this package wrote.
func pdfText(t *testing.T, raw []byte) []string {
	t.Helper()
	if !bytes.HasPrefix(raw, []byte("%PDF-1.4")) || !bytes.HasSuffix(raw, []byte("%%EOF\n")) {
		t.Fatal("not a PDF")
	}
	var pages []string
	streams := regexp.MustCompile(`<< /Length (\d+) /Filter /FlateDecode >>\nstream\n`)
	shown := regexp.MustCompile(`\(((?:\\.|[^\\)])*)\) Tj`)
	for _, loc := range streams.FindAllSubmatchIndex(raw, -1) {
		n, _ := strconv.Atoi(string(raw[loc[2]:loc[3]]))
		zr, err := zlib.NewReader(bytes.NewReader(raw[loc[1] : loc[1]+n]))
		if err != nil {
			t.Fatal(err)
		}
		plain, _ := io.ReadAll(zr)
		var sb strings.Builder
		for _, m := range shown.FindAllSubmatch(plain, -1) {
			sb.Write(m[1])
			sb.WriteByte('\n')
		}
		pages = append(pages, sb.String())
	}
	return pages
}

func TestReportExportPDF(t *testing.T) {
	h, sc := newExportFixture(t)
	rec := getReport(t, h, sc.ID, "?format=pdf&lang=fr")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("pdf export = %d %s: %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="dockyard-scan-team-app-1.0.pdf"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	pages := pdfText(t, rec.Body.Bytes())
	if len(pages) != 1 {
		t.Fatalf("pages = %d", len(pages))
	}
	text := pages[0]
	for _, want := range []string{
		"Rapport de vuln\xe9rabilit\xe9s", // French, in Windows-1252
		"team/app:1.0",
		sc.Digest,
		"Trivy 0.56.2",
		"CRITIQUE 1", "\xc9LEV\xc9E 1", "FAIBLE 1", "INCONNUE 1", "MOYENNE 0", // the console's words
		"CVE-2024-0001 *", // allowlisted
		"Page 1 sur 1",
		`log4j \(Log4Shell\): remote code execution \\ JNDI`, // escaped
	} {
		if !strings.Contains(text, want) {
			t.Errorf("PDF lacks %q", want)
		}
	}
	if strings.Contains(text, "CVE-2021-44228 *") {
		t.Error("an expired allowlist entry was shown as accepted")
	}
}

func TestReportExportPDFPaginates(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"Results":[{"Target":"t","Vulnerabilities":[`)
	for i := range 100 {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`{"VulnerabilityID":"CVE-2024-` + strconv.Itoa(1000+i) + `","PkgName":"p","Severity":"MEDIUM"}`)
	}
	sb.WriteString(`]}]}`)
	rows, err := reportRows([]byte(sb.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := renderPDF(&store.ScanResult{Name: "team/app", Reference: "1.0", Digest: "sha256:x"}, rows, "en", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	pages := pdfText(t, raw)
	perFirst, perNext := rowsBetween(firstTableY, bottomY), rowsBetween(nextTableY, bottomY)
	want := 1 + (100-perFirst+perNext-1)/perNext
	if len(pages) != want {
		t.Fatalf("pages = %d, want %d", len(pages), want)
	}
	seen := 0
	for i, p := range pages {
		seen += strings.Count(p, "CVE-2024-")
		if !strings.Contains(p, "VULNERABILITY") {
			t.Errorf("page %d has no table header", i+1)
		}
		if !strings.Contains(p, "Page "+strconv.Itoa(i+1)+" of "+strconv.Itoa(want)) {
			t.Errorf("page %d footer", i+1)
		}
	}
	if seen != 100 {
		t.Fatalf("rows printed = %d, want 100", seen)
	}
}

func TestReportExportEmptyAndErrors(t *testing.T) {
	st := openTestStore(t)
	sc, _ := st.EnqueueScan("library/alpine", "sha256:"+strings.Repeat("cd", 32), "sha256:"+strings.Repeat("cd", 32), "admin")
	gz, _ := gzipCompress([]byte(`{"Results":[]}`))
	if err := st.MarkScanSucceeded(sc.ID, 0, 0, 0, 0, 0, gz, ""); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(st, nil, nil, audit.New(st))

	rec := getReport(t, h, sc.ID, "?format=pdf")
	if rec.Code != http.StatusOK || !strings.Contains(strings.Join(pdfText(t, rec.Body.Bytes()), ""), "No vulnerabilities found.") {
		t.Fatalf("clean image PDF = %d", rec.Code)
	}
	// Scanned by digest: the file is named after the digest.
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="dockyard-scan-library-alpine-cdcdcdcdcdcd.pdf"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if rec := getReport(t, h, sc.ID, "?format=xlsx"); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown format = %d, want 400", rec.Code)
	}
	if rec := getReport(t, h, 9999, "?format=pdf"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown scan = %d, want 404", rec.Code)
	}
}
