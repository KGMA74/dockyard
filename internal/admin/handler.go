package admin

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"maestro/internal/storage"

	"github.com/labstack/echo/v4"
)

// gcBackend is the optional interface for local-only GC operations.
type gcBackend interface {
	AllBlobs() ([]string, error)
	BlobSize(digest string) (int64, error)
	ReferencedBlobs() (map[string]struct{}, error)
	RemoveBlob(digest string) error
	Root() string
}

type Handler struct {
	store   storage.Backend
	gcStore gcBackend // non-nil only for LocalBackend
}

func New(backend storage.Backend) *Handler {
	gc, _ := backend.(gcBackend)
	return &Handler{store: backend, gcStore: gc}
}

// GET /api/admin/repositories
func (h *Handler) GetRepositories(c echo.Context) error {
	repos, err := h.store.ListRepositories()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err500(err))
	}
	type repoInfo struct {
		Name  string   `json:"name"`
		Tags  []string `json:"tags"`
		Total int      `json:"total"`
	}
	result := make([]repoInfo, 0, len(repos))
	for _, name := range repos {
		tags, _ := h.store.ListTags(name)
		result = append(result, repoInfo{Name: name, Tags: tags, Total: len(tags)})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"repositories": result,
		"total":        len(result),
	})
}

// GET /api/admin/repositories/tags?name=<image>
func (h *Handler) GetTags(c echo.Context) error {
	name := c.QueryParam("name")
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "query param 'name' required"})
	}
	tags, err := h.store.ListTags(name)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err500(err))
	}
	type tagInfo struct {
		Tag    string `json:"tag"`
		Digest string `json:"digest"`
	}
	result := make([]tagInfo, 0, len(tags))
	for _, tag := range tags {
		_, digest, _ := h.store.GetManifest(name, tag)
		result = append(result, tagInfo{Tag: tag, Digest: digest})
	}
	return c.JSON(http.StatusOK, map[string]any{"name": name, "tags": result, "total": len(result)})
}

// DELETE /api/admin/repositories/manifests?name=<image>&digest=sha256:<hash>
func (h *Handler) DeleteManifest(c echo.Context) error {
	name := c.QueryParam("name")
	digest := c.QueryParam("digest")
	if name == "" || digest == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "params 'name' and 'digest' required"})
	}
	if !strings.HasPrefix(digest, "sha256:") {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "digest must start with sha256:"})
	}
	if err := h.store.DeleteManifest(name, digest); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

// GET /api/admin/storage/stats
func (h *Handler) StorageStats(c echo.Context) error {
	stats, err := h.store.Stats()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err500(err))
	}
	result := map[string]any{
		"total_size_bytes": stats.TotalSize,
		"total_size_human": humanSize(stats.TotalSize),
		"blob_count":       stats.BlobCount,
		"repo_count":       stats.RepoCount,
	}
	if h.gcStore != nil {
		result["storage_path"] = h.gcStore.Root()
	}
	return c.JSON(http.StatusOK, result)
}

// POST /api/admin/gc — removes blobs not referenced by any manifest
func (h *Handler) GarbageCollect(c echo.Context) error {
	if h.gcStore == nil {
		return c.JSON(http.StatusNotImplemented, map[string]string{
			"error": "garbage collection is only available with the local storage backend",
		})
	}
	referenced, err := h.gcStore.ReferencedBlobs()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err500(err))
	}
	allBlobs, err := h.gcStore.AllBlobs()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err500(err))
	}
	var freed int64
	var removed []string
	for _, digest := range allBlobs {
		if _, ok := referenced[digest]; ok {
			continue
		}
		size, _ := h.gcStore.BlobSize(digest)
		if err := h.gcStore.RemoveBlob(digest); err == nil {
			freed += size
			removed = append(removed, digest)
		}
	}
	return c.JSON(http.StatusOK, map[string]any{
		"removed":     removed,
		"count":       len(removed),
		"freed_bytes": freed,
		"freed_human": humanSize(freed),
	})
}

// GET /api/admin/storage/tree — storage tree for debugging
func (h *Handler) StorageTree(c echo.Context) error {
	if h.gcStore == nil {
		return c.JSON(http.StatusNotImplemented, map[string]string{
			"error": "storage tree is only available with the local storage backend",
		})
	}
	type entry struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
	}
	root := h.gcStore.Root()
	var entries []entry
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, _ := os.Stat(path)
		rel, _ := filepath.Rel(root, path)
		entries = append(entries, entry{Path: filepath.ToSlash(rel), Size: info.Size()})
		return nil
	})
	return c.JSON(http.StatusOK, map[string]any{"files": entries, "count": len(entries)})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func err500(err error) map[string]string { return map[string]string{"error": err.Error()} }

func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
