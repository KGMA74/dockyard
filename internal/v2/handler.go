package v2

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"dockyard/internal/events"
	"dockyard/internal/storage"
)

// Handler implements http.Handler for the Docker Registry V2 protocol.
// Image names containing slashes (org/image, org/sub/image) are supported via regex routing.
type Handler struct {
	store storage.Backend
	hub   *events.Hub
}

func New(backend storage.Backend, hub *events.Hub) *Handler {
	return &Handler{store: backend, hub: hub}
}

var (
	reCatalog       = regexp.MustCompile(`^/v2/_catalog$`)
	reTags          = regexp.MustCompile(`^/v2/(.+)/tags/list$`)
	reManifests     = regexp.MustCompile(`^/v2/(.+)/manifests/([^/]+)$`)
	reBlobGet       = regexp.MustCompile(`^/v2/(.+)/blobs/(sha256:[a-f0-9]+)$`)
	reBlobUploadNew = regexp.MustCompile(`^/v2/(.+)/blobs/uploads/$`)
	reBlobUpload    = regexp.MustCompile(`^/v2/(.+)/blobs/uploads/([^/]+)$`)
)

// statusRecorder wraps http.ResponseWriter to capture the written status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	start := time.Now()

	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
	path := r.URL.Path

	switch {
	case path == "/v2/" || path == "/v2":
		rec.WriteHeader(http.StatusOK)

	case reCatalog.MatchString(path) && r.Method == http.MethodGet:
		h.catalog(rec, r)

	case reTags.MatchString(path) && r.Method == http.MethodGet:
		m := reTags.FindStringSubmatch(path)
		h.tags(rec, r, m[1])

	case reManifests.MatchString(path):
		m := reManifests.FindStringSubmatch(path)
		h.manifests(rec, r, m[1], m[2])

	case reBlobGet.MatchString(path) && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		m := reBlobGet.FindStringSubmatch(path)
		h.getBlob(rec, r, m[1], m[2])

	case reBlobUploadNew.MatchString(path) && r.Method == http.MethodPost:
		m := reBlobUploadNew.FindStringSubmatch(path)
		h.initUpload(rec, r, m[1])

	case reBlobUpload.MatchString(path):
		m := reBlobUpload.FindStringSubmatch(path)
		h.patchOrCommitUpload(rec, r, m[1], m[2])

	default:
		registryError(rec, http.StatusNotFound, "UNSUPPORTED", "unsupported endpoint")
	}

	slog.Info("v2",
		"method", r.Method,
		"path", path,
		"status", rec.status,
		"duration_ms", time.Since(start).Milliseconds(),
	)
}

func (h *Handler) catalog(w http.ResponseWriter, _ *http.Request) {
	repos, err := h.store.ListRepositories()
	if err != nil {
		registryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
		return
	}
	jsonOK(w, map[string]any{"repositories": repos})
}

func (h *Handler) tags(w http.ResponseWriter, _ *http.Request, name string) {
	tags, err := h.store.ListTags(name)
	if err != nil {
		registryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
		return
	}
	jsonOK(w, map[string]any{"name": name, "tags": tags})
}

func (h *Handler) manifests(w http.ResponseWriter, r *http.Request, name, ref string) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		content, digest, err := h.store.GetManifest(name, ref)
		if err != nil {
			registryError(w, http.StatusNotFound, "MANIFEST_UNKNOWN", err.Error())
			return
		}
		w.Header().Set("Docker-Content-Digest", digest)
		w.Header().Set("Content-Type", mediaType(content))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(content)

	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			registryError(w, http.StatusBadRequest, "MANIFEST_INVALID", err.Error())
			return
		}
		hasher := sha256.New()
		hasher.Write(body)
		dgst := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
		if err := h.store.PutManifest(name, ref, dgst, body); err != nil {
			registryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
			return
		}
		w.Header().Set("Docker-Content-Digest", dgst)
		w.Header().Set("Location", "/v2/"+name+"/manifests/"+dgst)
		w.WriteHeader(http.StatusCreated)
		// Only tag pushes are notified — multi-arch pushes also PUT each
		// platform manifest by digest, which would just be repeat noise.
		if h.hub != nil && !strings.HasPrefix(ref, "sha256:") {
			h.hub.Publish(events.Event{Type: "push", Name: name, Tag: ref})
		}

	case http.MethodDelete:
		if err := h.store.DeleteManifest(name, ref); err != nil {
			registryError(w, http.StatusNotFound, "MANIFEST_UNKNOWN", err.Error())
			return
		}
		w.WriteHeader(http.StatusAccepted)

	default:
		registryError(w, http.StatusMethodNotAllowed, "UNSUPPORTED", "method not allowed")
	}
}

func (h *Handler) getBlob(w http.ResponseWriter, r *http.Request, name, digest string) {
	_ = name
	exists, err := h.store.BlobExists(digest)
	if err != nil || !exists {
		registryError(w, http.StatusNotFound, "BLOB_UNKNOWN", "blob unknown to registry")
		return
	}
	rc, size, err := h.store.GetBlob(digest)
	if err != nil {
		registryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
		return
	}
	defer rc.Close()
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
	w.Header().Set("Content-Type", "application/octet-stream")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	io.Copy(w, rc)
}

func (h *Handler) initUpload(w http.ResponseWriter, r *http.Request, name string) {
	// Monolithic push: digest provided in the POST query string
	if digest := r.URL.Query().Get("digest"); digest != "" {
		if err := h.store.PutBlob(digest, r.Body, r.ContentLength); err != nil {
			registryError(w, http.StatusBadRequest, "DIGEST_INVALID", err.Error())
			return
		}
		w.Header().Set("Docker-Content-Digest", digest)
		w.Header().Set("Location", "/v2/"+name+"/blobs/"+digest)
		w.WriteHeader(http.StatusCreated)
		return
	}
	// Chunked upload: generate UUID and initialize session
	b := make([]byte, 16)
	rand.Read(b)
	uuid := hex.EncodeToString(b)
	if err := h.store.InitUpload(uuid); err != nil {
		registryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
		return
	}
	w.Header().Set("Location", "/v2/"+name+"/blobs/uploads/"+uuid)
	w.Header().Set("Range", "0-0")
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) patchOrCommitUpload(w http.ResponseWriter, r *http.Request, name, id string) {
	switch r.Method {
	case http.MethodPatch:
		if err := h.store.AppendUpload(id, r.Body); err != nil {
			registryError(w, http.StatusNotFound, "BLOB_UPLOAD_UNKNOWN", err.Error())
			return
		}
		n, err := h.store.GetUploadSize(id)
		if err != nil {
			registryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
			return
		}
		w.Header().Set("Location", "/v2/"+name+"/blobs/uploads/"+id)
		w.Header().Set("Range", fmt.Sprintf("0-%d", n-1))
		w.WriteHeader(http.StatusAccepted)

	case http.MethodPut:
		digest := r.URL.Query().Get("digest")
		if digest == "" {
			registryError(w, http.StatusBadRequest, "DIGEST_INVALID", "digest query param required")
			return
		}
		if r.ContentLength > 0 {
			h.store.AppendUpload(id, r.Body)
		}
		if err := h.store.CommitUpload(id, digest); err != nil {
			registryError(w, http.StatusBadRequest, "DIGEST_INVALID", err.Error())
			return
		}
		w.Header().Set("Docker-Content-Digest", digest)
		w.Header().Set("Location", "/v2/"+name+"/blobs/"+digest)
		w.WriteHeader(http.StatusCreated)

	default:
		registryError(w, http.StatusMethodNotAllowed, "UNSUPPORTED", "method not allowed")
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func mediaType(content []byte) string {
	var m struct {
		MediaType string `json:"mediaType"`
	}
	if json.Unmarshal(content, &m) == nil && m.MediaType != "" {
		return m.MediaType
	}
	return "application/vnd.docker.distribution.manifest.v2+json"
}

func registryError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]string{{"code": code, "message": message}},
	})
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(v)
}

// IsV2Path reports whether a path belongs to the V2 protocol (used by the middleware).
func IsV2Path(path string) bool {
	return path == "/v2" || strings.HasPrefix(path, "/v2/")
}
