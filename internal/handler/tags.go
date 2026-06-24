package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handler) GetTags(c echo.Context) error {
	name := c.QueryParam("name")
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "query param 'name' is required"})
	}
	tags, err := h.client.Tags(name)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"name":  name,
		"tags":  tags,
		"total": len(tags),
	})
}
