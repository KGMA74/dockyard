package handler

import (
	"net/http"

	"maestro/internal/registry"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	client *registry.Client
}

func New(client *registry.Client) *Handler {
	return &Handler{client: client}
}

func (h *Handler) Health(c echo.Context) error {
	if err := h.client.Ping(); err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"status": "unreachable",
			"error":  err.Error(),
		})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) GetRepositories(c echo.Context) error {
	repos, err := h.client.Catalog()
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"repositories": repos,
		"total":        len(repos),
	})
}
