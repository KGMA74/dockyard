package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handler) GetManifest(c echo.Context) error {
	name := c.QueryParam("name")
	ref := c.QueryParam("ref")
	if name == "" || ref == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "query params 'name' and 'ref' are required",
		})
	}
	manifest, err := h.client.Manifest(name, ref)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, manifest)
}

func (h *Handler) DeleteManifest(c echo.Context) error {
	name := c.QueryParam("name")
	digest := c.QueryParam("digest")
	if name == "" || digest == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "query params 'name' and 'digest' are required",
		})
	}
	if err := h.client.DeleteManifest(name, digest); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}
