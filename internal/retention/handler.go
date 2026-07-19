package retention

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
)

// Handler exposes policy CRUD and plan preview/execution on
// /api/admin/retention (admin only, embedded/mirror modes).
type Handler struct {
	engine *Engine
	store  *store.Store
}

func NewHandler(engine *Engine, st *store.Store) *Handler {
	return &Handler{engine: engine, store: st}
}

// List — GET /api/admin/retention
func (h *Handler) List(c echo.Context) error {
	policies, err := h.store.ListRetentionPolicies()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if policies == nil {
		policies = []*store.RetentionPolicy{}
	}
	return c.JSON(http.StatusOK, map[string]any{"policies": policies, "count": len(policies)})
}

// Create — POST /api/admin/retention
func (h *Handler) Create(c echo.Context) error {
	var body store.RetentionPolicy
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	body.Enabled = true
	p, err := h.store.CreateRetentionPolicy(body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, p)
}

// Delete — DELETE /api/admin/retention/:id
func (h *Handler) Delete(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid policy id"})
	}
	if err := h.store.DeleteRetentionPolicy(id); err != nil {
		if errors.Is(err, store.ErrPolicyNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "policy not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "policy deleted"})
}

// Run — POST /api/admin/retention/run[?dryRun=true]. Dry run returns the plan
// without touching anything; a real run applies it and reports what happened.
func (h *Handler) Run(c echo.Context) error {
	plan, err := h.engine.Evaluate(time.Now())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	dryRun := c.QueryParam("dryRun") == "true"
	deleted := 0
	if !dryRun {
		deleted, _ = h.engine.Apply(plan)
	}
	return c.JSON(http.StatusOK, map[string]any{
		"plan":    plan,
		"dry_run": dryRun,
		"deleted": deleted,
	})
}
