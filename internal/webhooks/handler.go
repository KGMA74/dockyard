package webhooks

import (
	"errors"
	"net/http"
	"strconv"

	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
)

// Handler exposes webhook CRUD + test delivery on /api/admin/webhooks
// (admin only, every mode).
type Handler struct {
	store      *store.Store
	dispatcher *Dispatcher
}

func NewHandler(st *store.Store, d *Dispatcher) *Handler {
	return &Handler{store: st, dispatcher: d}
}

// List — GET /api/admin/webhooks
func (h *Handler) List(c echo.Context) error {
	hooks, err := h.store.ListWebhooks()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if hooks == nil {
		hooks = []*store.Webhook{}
	}
	return c.JSON(http.StatusOK, map[string]any{"webhooks": hooks, "count": len(hooks)})
}

// Create — POST /api/admin/webhooks
func (h *Handler) Create(c echo.Context) error {
	var body struct {
		URL    string   `json:"url"`
		Secret string   `json:"secret"`
		Events []string `json:"events"`
		Format string   `json:"format"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	hook, err := h.store.CreateWebhook(store.Webhook{
		URL: body.URL, Secret: body.Secret, Events: body.Events, Format: body.Format, Enabled: true,
	})
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, hook)
}

// Delete — DELETE /api/admin/webhooks/:id
func (h *Handler) Delete(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid webhook id"})
	}
	if err := h.store.DeleteWebhook(id); err != nil {
		if errors.Is(err, store.ErrWebhookNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "webhook not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "webhook deleted"})
}

// Test — POST /api/admin/webhooks/:id/test — synchronous test delivery so the
// admin sees the outcome immediately.
func (h *Handler) Test(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid webhook id"})
	}
	hook, err := h.store.WebhookByID(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "webhook not found"})
	}
	if err := h.dispatcher.DeliverNow(hook); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "test event delivered"})
}
