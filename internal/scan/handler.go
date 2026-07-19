package scan

import (
	"errors"
	"net/http"
	"strconv"

	"dockyard/internal/audit"
	"dockyard/internal/auth"
	"dockyard/internal/registry"
	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
)

// manifestResolver resolves a name+reference to the manifest digest that
// will actually be scanned. storage.Backend already satisfies this shape;
// registry.Client is adapted via RegistryResolver.
type manifestResolver interface {
	GetManifest(name, reference string) ([]byte, string, error)
}

// RegistryResolver adapts registry.Client (proxy mode) to manifestResolver.
type RegistryResolver struct {
	Client *registry.Client
}

func (r RegistryResolver) GetManifest(name, reference string) ([]byte, string, error) {
	return r.Client.RawManifest(name, reference)
}

// Handler exposes scan trigger/list/get/report on /api/admin/scans (admin
// only, every mode).
type Handler struct {
	store      *store.Store
	dispatcher *Dispatcher
	resolver   manifestResolver
	auditor    *audit.Recorder
}

func NewHandler(st *store.Store, d *Dispatcher, resolver manifestResolver, auditor *audit.Recorder) *Handler {
	return &Handler{store: st, dispatcher: d, resolver: resolver, auditor: auditor}
}

// Trigger — POST /api/admin/scans {"name","reference"}
func (h *Handler) Trigger(c echo.Context) error {
	var body struct {
		Name      string `json:"name"`
		Reference string `json:"reference"`
	}
	if err := c.Bind(&body); err != nil || body.Name == "" || body.Reference == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name and reference are required"})
	}

	_, digest, err := h.resolver.GetManifest(body.Name, body.Reference)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "manifest not found"})
	}

	actor := ""
	if p, ok := auth.CurrentPrincipal(c); ok {
		actor = p.Username
	}

	res, err := h.dispatcher.Enqueue(body.Name, body.Reference, digest, actor)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	// The generic admin audit middleware only captures query params, and this
	// endpoint takes a JSON body — record explicitly for parity with
	// push/delete audit richness.
	status := "queued"
	if res.Cached {
		status = "cached"
	}
	h.auditor.Record(actor, "scan", body.Name, body.Reference, c.RealIP(), status, digest)

	return c.JSON(http.StatusAccepted, map[string]any{"scan": res.Scan, "cached": res.Cached})
}

// List — GET /api/admin/scans?name=&digest=&limit=&offset=
func (h *Handler) List(c echo.Context) error {
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	offset, _ := strconv.Atoi(c.QueryParam("offset"))
	scans, total, err := h.store.ListScans(c.QueryParam("name"), c.QueryParam("digest"), limit, offset)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if scans == nil {
		scans = []*store.ScanResult{}
	}
	return c.JSON(http.StatusOK, map[string]any{"scans": scans, "count": total})
}

// Get — GET /api/admin/scans/:id
func (h *Handler) Get(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid scan id"})
	}
	sc, err := h.store.ScanByID(id)
	if errors.Is(err, store.ErrScanNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "scan not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, sc)
}

// Report — GET /api/admin/scans/:id/report
func (h *Handler) Report(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid scan id"})
	}
	report, err := h.store.ScanReport(id)
	if errors.Is(err, store.ErrScanNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "report not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.Blob(http.StatusOK, "application/json", report)
}
