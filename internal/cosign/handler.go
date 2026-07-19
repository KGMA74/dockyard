package cosign

import (
	"errors"
	"net/http"
	"strconv"

	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
)

// Handler exposes signed-push status and per-repo policy overrides on
// /api/admin/signing (admin only, every mode).
type Handler struct {
	store  *store.Store
	policy *Policy
}

func NewHandler(st *store.Store, policy *Policy) *Handler {
	return &Handler{store: st, policy: policy}
}

// Status — GET /api/admin/signing
func (h *Handler) Status(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{
		"enabled":     h.policy.defaultRequired,
		"keys_loaded": len(h.policy.keys),
	})
}

// List — GET /api/admin/signing/policies
func (h *Handler) List(c echo.Context) error {
	policies, err := h.store.ListSigningPolicies()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if policies == nil {
		policies = []*store.SigningPolicy{}
	}
	return c.JSON(http.StatusOK, map[string]any{"policies": policies, "count": len(policies)})
}

// Create — POST /api/admin/signing/policies
func (h *Handler) Create(c echo.Context) error {
	var body struct {
		RepoPattern string `json:"repo_pattern"`
		Required    bool   `json:"required"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	p, err := h.store.CreateSigningPolicy(body.RepoPattern, body.Required)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, p)
}

// Delete — DELETE /api/admin/signing/policies/:id
func (h *Handler) Delete(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid policy id"})
	}
	if err := h.store.DeleteSigningPolicy(id); err != nil {
		if errors.Is(err, store.ErrSigningPolicyNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "policy not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "policy deleted"})
}
