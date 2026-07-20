package admin

import (
	"net/http"
	"strings"

	"dockyard/internal/cosign"
	"dockyard/internal/registry"

	"github.com/labstack/echo/v4"
)

// RemoteHandler expose les mêmes routes admin mais en interrogeant
// une registry distante via l'API V2. GC et stats de stockage ne sont pas disponibles.
type RemoteHandler struct {
	client  *registry.Client
	signing *cosign.Policy
}

func NewRemote(client *registry.Client, signing *cosign.Policy) *RemoteHandler {
	return &RemoteHandler{client: client, signing: signing}
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

// GET /api/admin/repositories/manifest?name=<image>&reference=<tag-or-digest>
func (h *RemoteHandler) GetManifestDetails(c echo.Context) error {
	name := c.QueryParam("name")
	reference := c.QueryParam("reference")
	if name == "" || reference == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "params 'name' and 'reference' required"})
	}
	result, err := h.manifestDetails(name, reference)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, result)
}

// manifestDetails resolves reference and parses its manifest, tagging the
// result with signed status when cosign keys are configured. Shared by
// GetManifestDetails and GetTagDiff.
func (h *RemoteHandler) manifestDetails(name, reference string) (map[string]any, error) {
	raw, digest, err := h.client.RawManifest(name, reference)
	if err != nil {
		return nil, err
	}
	result, err := parseManifestDetails(raw, digest, func(blobDigest string) ([]byte, error) {
		return h.client.Blob(name, blobDigest)
	}, func(manifestDigest string) ([]byte, error) {
		childRaw, _, err := h.client.RawManifest(name, manifestDigest)
		return childRaw, err
	})
	if err != nil {
		return nil, err
	}
	if h.signing.HasKeys() {
		result["signed"] = h.signing.Signed(cosign.ClientFetcher{Client: h.client}, name, digest)
	}
	return result, nil
}

// GET /api/admin/repositories/diff?name=<image>&reference_a=<tag-or-digest>&reference_b=<tag-or-digest>
func (h *RemoteHandler) GetTagDiff(c echo.Context) error {
	name := c.QueryParam("name")
	refA := c.QueryParam("reference_a")
	refB := c.QueryParam("reference_b")
	if name == "" || refA == "" || refB == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "params 'name', 'reference_a' and 'reference_b' required"})
	}
	a, err := h.manifestDetails(name, refA)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "reference_a: " + err.Error()})
	}
	b, err := h.manifestDetails(name, refB)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "reference_b: " + err.Error()})
	}
	return c.JSON(http.StatusOK, diffManifests(a, b))
}

// GET /api/admin/repositories/layer?name=<image>&digest=sha256:<layer-digest>
func (h *RemoteHandler) GetLayerEntries(c echo.Context) error {
	name := c.QueryParam("name")
	digest := c.QueryParam("digest")
	if name == "" || digest == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "params 'name' and 'digest' required"})
	}
	if entries, ok := layerEntriesCache.Get(digest); ok {
		return c.JSON(http.StatusOK, map[string]any{"digest": digest, "entries": entries, "count": len(entries)})
	}
	rc, err := h.client.BlobStream(name, digest)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	defer func() { _ = rc.Close() }()
	entries, err := parseLayerEntries(rc)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	layerEntriesCache.Add(digest, entries)
	return c.JSON(http.StatusOK, map[string]any{"digest": digest, "entries": entries, "count": len(entries)})
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

// DELETE /api/admin/repositories?name=<image>
// La registry distante n'a pas de suppression de dépôt : on supprime
// le manifest de chaque tag, un par un.
func (h *RemoteHandler) DeleteRepository(c echo.Context) error {
	name := c.QueryParam("name")
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "query param 'name' required"})
	}
	tags, err := h.client.Tags(name)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	deleted := 0
	for _, tag := range tags {
		m, err := h.client.Manifest(name, tag)
		if err != nil || m.Digest == "" {
			continue
		}
		if err := h.client.DeleteManifest(name, m.Digest); err == nil {
			deleted++
		}
	}
	if deleted == 0 && len(tags) > 0 {
		return c.JSON(http.StatusBadGateway, map[string]string{
			"error": "no manifest could be deleted (upstream may forbid deletion)",
		})
	}
	return c.NoContent(http.StatusNoContent)
}

// NotSupported renvoie 501 pour les opérations indisponibles en mode proxy.
func NotSupported(c echo.Context) error {
	return c.JSON(http.StatusNotImplemented, map[string]string{
		"error": "operation not available in proxy mode (no local storage access)",
	})
}
