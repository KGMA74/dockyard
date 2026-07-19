package scan

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"dockyard/internal/audit"
	"dockyard/internal/storage"
	"dockyard/internal/storage/storagetest"
	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
)

// newTestHandler wires a Handler against a local storage backend (as the
// manifest resolver) and a dispatcher whose trivy binary is this test binary
// re-exec'd via TestMain, same as scan_test.go.
func newTestHandler(t *testing.T) (*Handler, *storage.LocalBackend, *store.Store) {
	t.Helper()
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st := openTestStore(t)
	d := newTestDispatcher(t, st, nil)
	return NewHandler(st, d, backend, audit.New(st)), backend, st
}

func pushTestManifest(t *testing.T, backend *storage.LocalBackend, name, tag string) string {
	t.Helper()
	config := []byte(`{"architecture":"amd64"}`)
	configDgst := storagetest.Digest(config)
	if err := backend.PutBlob(configDgst, bytes.NewReader(config), int64(len(config))); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"schemaVersion":2,"config":{"digest":"` + configDgst + `"},"layers":[]}`)
	digest := storagetest.Digest(manifest)
	if err := backend.PutManifest(name, tag, digest, manifest); err != nil {
		t.Fatal(err)
	}
	return digest
}

func TestHandlerTriggerAndGet(t *testing.T) {
	h, backend, st := newTestHandler(t)
	t.Setenv("HELPER_MODE", "report")
	t.Setenv("HELPER_REPORT", `{"Results":[{"Vulnerabilities":[{"Severity":"LOW"}]}]}`)

	digest := pushTestManifest(t, backend, "library/nginx", "latest")

	e := echo.New()
	body, _ := json.Marshal(map[string]string{"name": "library/nginx", "reference": "latest"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/scans", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	if err := h.Trigger(e.NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("Trigger status = %d: %s", rec.Code, rec.Body.String())
	}
	var triggerResp struct {
		Scan   store.ScanResult `json:"scan"`
		Cached bool              `json:"cached"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &triggerResp); err != nil {
		t.Fatal(err)
	}
	if triggerResp.Cached {
		t.Fatal("expected a fresh scan")
	}
	if triggerResp.Scan.Digest != digest {
		t.Fatalf("resolved digest = %q, want %q", triggerResp.Scan.Digest, digest)
	}

	waitForStatus(t, st, triggerResp.Scan.ID, "succeeded")

	getReq := httptest.NewRequest(http.MethodGet, "/api/admin/scans/"+itoa(triggerResp.Scan.ID), nil)
	getRec := httptest.NewRecorder()
	getCtx := e.NewContext(getReq, getRec)
	getCtx.SetParamNames("id")
	getCtx.SetParamValues(itoa(triggerResp.Scan.ID))
	if err := h.Get(getCtx); err != nil {
		t.Fatal(err)
	}
	if getRec.Code != http.StatusOK {
		t.Fatalf("Get status = %d: %s", getRec.Code, getRec.Body.String())
	}
	var got store.ScanResult
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.LowCount != 1 {
		t.Fatalf("LowCount = %d, want 1", got.LowCount)
	}
}

func TestHandlerTriggerUnknownManifest(t *testing.T) {
	h, _, _ := newTestHandler(t)
	e := echo.New()
	body, _ := json.Marshal(map[string]string{"name": "library/missing", "reference": "latest"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/scans", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	if err := h.Trigger(e.NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandlerGetUnknownID(t *testing.T) {
	h, _, _ := newTestHandler(t)
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/scans/999", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("id")
	ctx.SetParamValues("999")
	if err := h.Get(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandlerReportReturnsDecompressedJSON(t *testing.T) {
	h, backend, st := newTestHandler(t)
	t.Setenv("HELPER_MODE", "report")
	t.Setenv("HELPER_REPORT", `{"Results":[]}`)
	pushTestManifest(t, backend, "library/nginx", "latest")

	e := echo.New()
	body, _ := json.Marshal(map[string]string{"name": "library/nginx", "reference": "latest"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/scans", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	if err := h.Trigger(e.NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	var triggerResp struct {
		Scan store.ScanResult `json:"scan"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &triggerResp); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, st, triggerResp.Scan.ID, "succeeded")

	reportReq := httptest.NewRequest(http.MethodGet, "/api/admin/scans/"+itoa(triggerResp.Scan.ID)+"/report", nil)
	reportRec := httptest.NewRecorder()
	reportCtx := e.NewContext(reportReq, reportRec)
	reportCtx.SetParamNames("id")
	reportCtx.SetParamValues(itoa(triggerResp.Scan.ID))
	if err := h.Report(reportCtx); err != nil {
		t.Fatal(err)
	}
	if reportRec.Code != http.StatusOK {
		t.Fatalf("Report status = %d: %s", reportRec.Code, reportRec.Body.String())
	}
	if reportRec.Body.String() != `{"Results":[]}` {
		t.Fatalf("Report body = %q", reportRec.Body.String())
	}
}

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}
