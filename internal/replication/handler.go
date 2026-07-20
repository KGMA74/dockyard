package replication

import (
	"errors"
	"net/http"
	"strconv"

	"dockyard/internal/registry"
	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
)

// Handler exposes replication target CRUD + test on
// /api/admin/replication/targets (admin only, embedded/mirror mode).
type Handler struct {
	store      *store.Store
	replicator *Replicator
}

func NewHandler(st *store.Store, r *Replicator) *Handler {
	return &Handler{store: st, replicator: r}
}

// List — GET /api/admin/replication/targets
func (h *Handler) List(c echo.Context) error {
	targets, err := h.store.ListReplicationTargets()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if targets == nil {
		targets = []*store.ReplicationTarget{}
	}
	return c.JSON(http.StatusOK, map[string]any{"targets": targets, "count": len(targets)})
}

// Create — POST /api/admin/replication/targets
func (h *Handler) Create(c echo.Context) error {
	var body struct {
		Name        string `json:"name"`
		BaseURL     string `json:"base_url"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		RepoPattern string `json:"repo_pattern"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	target, err := h.store.CreateReplicationTarget(store.ReplicationTarget{
		Name: body.Name, BaseURL: body.BaseURL, Username: body.Username,
		Password: body.Password, RepoPattern: body.RepoPattern, Enabled: true,
	})
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, target)
}

// Delete — DELETE /api/admin/replication/targets/:id
func (h *Handler) Delete(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid target id"})
	}
	if err := h.store.DeleteReplicationTarget(id); err != nil {
		if errors.Is(err, store.ErrReplicationTargetNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "target not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "target deleted"})
}

// Test — POST /api/admin/replication/targets/:id/test — pings the target's
// V2 endpoint and reports reachability, without queuing any replication.
func (h *Handler) Test(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid target id"})
	}
	target, err := h.store.ReplicationTargetByID(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "target not found"})
	}
	client := registry.NewClient(target.BaseURL, target.Username, target.Password)
	if err := client.Ping(); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "target reachable"})
}
