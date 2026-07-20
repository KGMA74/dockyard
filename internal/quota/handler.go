// Package quota exposes admin CRUD for per-repo/per-user byte quotas and
// their current usage on /api/admin/quotas (admin only, every mode — it's
// pure SQLite policy data, like signing policies). Enforcement itself
// happens in internal/v2, embedded mode only, since that's the only mode
// where Dockyard owns the storage write path.
package quota

import (
	"errors"
	"net/http"
	"strconv"

	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	store *store.Store
}

func NewHandler(st *store.Store) *Handler {
	return &Handler{store: st}
}

// List — GET /api/admin/quotas
func (h *Handler) List(c echo.Context) error {
	quotas, err := h.store.ListQuotas()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if quotas == nil {
		quotas = []*store.Quota{}
	}
	usage, err := h.store.ListQuotaUsage()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if usage == nil {
		usage = []*store.QuotaUsage{}
	}
	return c.JSON(http.StatusOK, map[string]any{"quotas": quotas, "usage": usage})
}

// Set — PUT /api/admin/quotas
func (h *Handler) Set(c echo.Context) error {
	var body struct {
		ScopeType   string `json:"scope_type"`
		ScopeValue  string `json:"scope_value"`
		MaxBytes    int64  `json:"max_bytes"`
		WarnPercent int    `json:"warn_percent"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	q, err := h.store.SetQuota(body.ScopeType, body.ScopeValue, body.MaxBytes, body.WarnPercent)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, q)
}

// Delete — DELETE /api/admin/quotas/:id
func (h *Handler) Delete(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid quota id"})
	}
	if err := h.store.DeleteQuota(id); err != nil {
		if errors.Is(err, store.ErrQuotaNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "quota not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "quota deleted"})
}

// ResetUsage — POST /api/admin/quotas/usage/reset
func (h *Handler) ResetUsage(c echo.Context) error {
	var body struct {
		ScopeType  string `json:"scope_type"`
		ScopeValue string `json:"scope_value"`
	}
	if err := c.Bind(&body); err != nil || body.ScopeType == "" || body.ScopeValue == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "scope_type and scope_value are required"})
	}
	if err := h.store.ResetQuotaUsage(body.ScopeType, body.ScopeValue); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "usage reset"})
}
