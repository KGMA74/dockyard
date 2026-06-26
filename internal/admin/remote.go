package admin

import (
	"net/http"
	"strings"

	"dockyard/internal/registry"

	"github.com/labstack/echo/v4"
)

// RemoteHandler expose les mêmes routes admin mais en interrogeant
// une registry distante via l'API V2. GC et stats de stockage ne sont pas disponibles.
type RemoteHandler struct {
	client *registry.Client
}

func NewRemote(client *registry.Client) *RemoteHandler {
	return &RemoteHandler{client: client}
}

// GET /api/admin/repositories
func (h *RemoteHandler) GetRepositories(c echo.Context) error {
	repos, err := h.client.Catalog()
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	type repoInfo struct {
		Name  string   `json:"name"`
		Tags  []string `json:"tags"`
		Total int      `json:"total"`
	}
	result := make([]repoInfo, 0, len(repos))
	for _, name := range repos {
		tags, _ := h.client.Tags(name)
		result = append(result, repoInfo{Name: name, Tags: tags, Total: len(tags)})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"repositories": result,
		"total":        len(result),
		"mode":         "proxy",
	})
}

// GET /api/admin/repositories/tags?name=<image>
func (h *RemoteHandler) GetTags(c echo.Context) error {
	name := c.QueryParam("name")
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "query param 'name' required"})
	}
	tags, err := h.client.Tags(name)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	type tagInfo struct {
		Tag    string `json:"tag"`
		Digest string `json:"digest"`
	}
	result := make([]tagInfo, 0, len(tags))
	for _, tag := range tags {
		m, _ := h.client.Manifest(name, tag)
		var digest string
		if m != nil {
			digest = m.Digest
		}
		result = append(result, tagInfo{Tag: tag, Digest: digest})
	}
	return c.JSON(http.StatusOK, map[string]any{"name": name, "tags": result, "total": len(result)})
}

// DELETE /api/admin/repositories/manifests?name=<image>&digest=sha256:<hash>
func (h *RemoteHandler) DeleteManifest(c echo.Context) error {
	name := c.QueryParam("name")
	digest := c.QueryParam("digest")
	if name == "" || digest == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "params 'name' and 'digest' required"})
	}
	if !strings.HasPrefix(digest, "sha256:") {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "digest must start with sha256:"})
	}
	if err := h.client.DeleteManifest(name, digest); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

// NotSupported renvoie 501 pour les opérations indisponibles en mode proxy.
func NotSupported(c echo.Context) error {
	return c.JSON(http.StatusNotImplemented, map[string]string{
		"error": "operation not available in proxy mode (no local storage access)",
	})
}
