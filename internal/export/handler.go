package export

import (
	"fmt"
	"net/http"

	"dockyard/internal/storage"

	"github.com/labstack/echo/v4"
)

// Handler exposes repository export/import (admin only, embedded/mirror).
type Handler struct {
	backend storage.Backend
}

func NewHandler(backend storage.Backend) *Handler { return &Handler{backend: backend} }

// Export — GET /api/admin/repositories/export?name=<repo>
// Streams an OCI image-layout tarball.
func (h *Handler) Export(c echo.Context) error {
	name := c.QueryParam("name")
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "param 'name' required"})
	}
	// Verify everything is exportable before committing to a 200 stream.
	if err := Preflight(h.backend, name); err != nil {
		return c.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
	}
	filename := fmt.Sprintf("%s.oci.tar", sanitizeFilename(name))
	c.Response().Header().Set(echo.HeaderContentType, "application/x-tar")
	c.Response().Header().Set(echo.HeaderContentDisposition, `attachment; filename="`+filename+`"`)
	c.Response().WriteHeader(http.StatusOK)
	if err := Export(c.Response(), h.backend, name); err != nil {
		// Headers already sent — the truncated tar signals the failure.
		c.Logger().Error("export failed: ", err)
	}
	return nil
}

// Import — POST /api/admin/repositories/import?name=<repo>
// Body: an OCI image-layout tarball.
func (h *Handler) Import(c echo.Context) error {
	name := c.QueryParam("name")
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "param 'name' required"})
	}
	tags, err := Import(c.Request().Body, h.backend, name)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{"message": "import complete", "tags": tags})
}

func sanitizeFilename(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}
