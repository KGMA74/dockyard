package v2

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"dockyard/internal/auth"
	"dockyard/internal/cosign"
	"dockyard/internal/events"
	"dockyard/internal/reference"
	"dockyard/internal/storage"
	"dockyard/internal/store"
	"dockyard/internal/tracing"

	"go.opentelemetry.io/otel/attribute"
)

// Handler implements http.Handler for the Docker Registry V2 protocol.
// Image names containing slashes (org/image, org/sub/image) are supported via regex routing.
type Handler struct {
	store   storage.Backend
	hub     *events.Hub
	onPull  func(name, reference string)
	signing *cosign.Policy // nil = signed-push enforcement off
	db      *store.Store   // used for quota enforcement; nil disables it
}

func New(backend storage.Backend, hub *events.Hub, signing *cosign.Policy, db *store.Store) *Handler {
	return &Handler{store: backend, hub: hub, signing: signing, db: db}
}

// OnPull registers a callback fired after each successful manifest GET — the
// event that constitutes "a pull". The callback must not block (the pull
// tracker batches asynchronously).
func (h *Handler) OnPull(fn func(name, reference string)) { h.onPull = fn }

// maxManifestBytes caps a pushed manifest. Manifests are buffered in memory
// and hashed, so an unbounded body is a trivial memory-exhaustion vector;
// 4 MiB is the limit the reference registry uses as well.
const maxManifestBytes = 4 << 20

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
	route := reference.ParseV2Path(path)

	// Repository names, tags, digests and upload ids all end up in storage
	// paths: refuse malformed ones before any handler sees them.
	if err := route.Validate(); err != nil {
		registryError(rec, http.StatusBadRequest, reference.V2ErrorCode(err), err.Error())
		logV2(r, rec.status, start)
		return
	}

	get := r.Method == http.MethodGet
	getOrHead := get || r.Method == http.MethodHead

	switch {
	case route.Route == reference.RouteBase:
		rec.WriteHeader(http.StatusOK)

	case route.Route == reference.RouteCatalog && get:
		h.catalog(rec, r)

	case route.Route == reference.RouteTags && get:
		h.tags(rec, r, route.Name)

	case route.Route == reference.RouteManifest:
		h.manifests(rec, r, route.Name, route.Ref)

	case route.Route == reference.RouteBlob && getOrHead:
		h.getBlob(rec, r, route.Name, route.Ref)

	case route.Route == reference.RouteUploadInit && r.Method == http.MethodPost:
		h.initUpload(rec, r, route.Name)

	case route.Route == reference.RouteUpload:
		h.patchOrCommitUpload(rec, r, route.Name, route.Ref)

	default:
		registryError(rec, http.StatusNotFound, "UNSUPPORTED", "unsupported endpoint")
	}

	logV2(r, rec.status, start)
}

func logV2(r *http.Request, status int, start time.Time) {
	slog.Info("v2",
		"method", r.Method,
		"path", r.URL.Path,
		"status", status,
		"duration_ms", time.Since(start).Milliseconds(),
	)
}

// catalog lists repositories. A principal restricted to repository patterns
// only sees the repositories those patterns cover.
func (h *Handler) catalog(w http.ResponseWriter, r *http.Request) {
	repos, err := h.store.ListRepositories()
	if err != nil {
		registryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
		return
	}
	if p, ok := auth.PrincipalFromRequest(r); ok && len(p.RepoPatterns) > 0 {
		visible := make([]string, 0, len(repos))
		for _, repo := range repos {
			if auth.MatchesRepo(p.RepoPatterns, repo) {
				visible = append(visible, repo)
			}
		}
		repos = visible
	}
	if repos == nil {
		repos = []string{}
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
		_, end := tracing.StartSpan(r.Context(), "GetManifest", attribute.String("repository", name), attribute.String("reference", ref))
		content, digest, err := h.store.GetManifest(name, ref)
		end(err)
		if err != nil {
			registryError(w, http.StatusNotFound, "MANIFEST_UNKNOWN", err.Error())
			return
		}
		w.Header().Set("Docker-Content-Digest", digest)
		w.Header().Set("Content-Type", mediaType(content))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		// The scanner pulling an image to scan it is not someone using it.
		if p, ok := auth.PrincipalFromRequest(r); h.onPull != nil && r.Method == http.MethodGet && (!ok || !p.Scanner) {
			h.onPull(name, ref)
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)

	case http.MethodPut:
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxManifestBytes))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				registryError(w, http.StatusRequestEntityTooLarge, "MANIFEST_INVALID",
					fmt.Sprintf("manifest exceeds the %d byte limit", maxManifestBytes))
				return
			}
			registryError(w, http.StatusBadRequest, "MANIFEST_INVALID", err.Error())
			return
		}
		if !json.Valid(body) {
			registryError(w, http.StatusBadRequest, "MANIFEST_INVALID", "manifest is not valid JSON")
			return
		}
		hasher := sha256.New()
		hasher.Write(body)
		dgst := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
		// A push by digest must carry exactly the content that digest names.
		// Unchecked, the S3 backend stored the body under the *claimed*
		// digest key: any pusher could overwrite an existing manifest with
		// different content, breaking pulls by digest and un-referencing the
		// original image's layers for the next GC.
		if reference.IsDigest(ref) && ref != dgst {
			registryError(w, http.StatusBadRequest, "DIGEST_INVALID",
				fmt.Sprintf("manifest content hashes to %s, not %s", dgst, ref))
			return
		}
		// Signed-push policy applies only to "real" tag pushes — pushes by
		// digest and cosign's own signature/attestation/sbom tags are
		// exempt, since cosign must be able to attach a signature to an
		// image that isn't tagged yet (push by digest, sign, then tag).
		if !strings.HasPrefix(ref, "sha256:") && !cosign.IsArtifactTag(ref) {
			if err := h.signing.Enforce(cosign.BackendFetcher{Backend: h.store}, name, dgst); err != nil {
				registryError(w, http.StatusForbidden, "DENIED", err.Error())
				return
			}
		}
		_, end := tracing.StartSpan(r.Context(), "PutManifest", attribute.String("repository", name), attribute.String("reference", ref))
		err = h.store.PutManifest(name, ref, dgst, body)
		end(err)
		if err != nil {
			registryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
			return
		}
		w.Header().Set("Docker-Content-Digest", dgst)
		w.Header().Set("Location", "/v2/"+name+"/manifests/"+dgst)
		w.WriteHeader(http.StatusCreated)
		// Only tag pushes are notified — multi-arch pushes also PUT each
		// platform manifest by digest, which would just be repeat noise.
		if h.hub != nil && !strings.HasPrefix(ref, "sha256:") {
			h.hub.Publish(events.Event{Type: "push", Name: name, Tag: ref, Actor: requestActor(r)})
		}

	case http.MethodDelete:
		_, end := tracing.StartSpan(r.Context(), "DeleteManifest", attribute.String("repository", name), attribute.String("reference", ref))
		err := h.store.DeleteManifest(name, ref)
		end(err)
		if err != nil {
			registryError(w, http.StatusNotFound, "MANIFEST_UNKNOWN", err.Error())
			return
		}
		if h.hub != nil {
			h.hub.Publish(events.Event{Type: "delete", Name: name, Tag: ref, Actor: requestActor(r)})
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
	_, end := tracing.StartSpan(r.Context(), "GetBlob", attribute.String("digest", digest))
	rc, size, err := h.store.GetBlob(digest)
	end(err)
	if err != nil {
		registryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
		return
	}
	defer func() { _ = rc.Close() }()
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
	w.Header().Set("Content-Type", "application/octet-stream")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

func (h *Handler) initUpload(w http.ResponseWriter, r *http.Request, name string) {
	// Monolithic push: digest provided in the POST query string
	if digest := r.URL.Query().Get("digest"); digest != "" {
		if !reference.ValidDigest(digest) {
			registryError(w, http.StatusBadRequest, "DIGEST_INVALID", "digest must be sha256:<64 hex chars>")
			return
		}
		// Content-Length is unset for chunked-transfer requests — quota is
		// only enforced when the size is known up front. Chunked uploads are
		// covered at commit time in patchOrCommitUpload instead.
		if r.ContentLength > 0 {
			if blocked, warnings, err := h.reserveQuota(name, requestActor(r), r.ContentLength); err != nil {
				registryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
				return
			} else if blocked != nil {
				registryError(w, http.StatusInsufficientStorage, "DENIED", blocked.Error())
				return
			} else {
				h.publishQuotaWarnings(warnings, requestActor(r))
			}
		}
		_, end := tracing.StartSpan(r.Context(), "PutBlob", attribute.String("digest", digest))
		err := h.store.PutBlob(digest, r.Body, r.ContentLength)
		end(err)
		if err != nil {
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
	_, _ = rand.Read(b) // crypto/rand never fails on supported platforms
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
		if !reference.ValidDigest(digest) {
			registryError(w, http.StatusBadRequest, "DIGEST_INVALID", "digest query param must be sha256:<64 hex chars>")
			return
		}
		if r.ContentLength > 0 {
			if err := h.store.AppendUpload(id, r.Body); err != nil {
				registryError(w, http.StatusNotFound, "BLOB_UPLOAD_UNKNOWN", err.Error())
				return
			}
		}
		if size, sizeErr := h.store.GetUploadSize(id); sizeErr == nil && size > 0 {
			if blocked, warnings, err := h.reserveQuota(name, requestActor(r), size); err != nil {
				registryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
				return
			} else if blocked != nil {
				_ = h.store.DeleteUpload(id)
				registryError(w, http.StatusInsufficientStorage, "DENIED", blocked.Error())
				return
			} else {
				h.publishQuotaWarnings(warnings, requestActor(r))
			}
		}
		_, end := tracing.StartSpan(r.Context(), "CommitUpload", attribute.String("digest", digest))
		err := h.store.CommitUpload(id, digest)
		end(err)
		if err != nil {
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

// reserveQuota checks the incoming blob's size against any configured
// repo/user quota and, if both have room, atomically records the usage. A
// nil db (quotas not wired up, e.g. proxy mode) makes every push unlimited.
func (h *Handler) reserveQuota(name, username string, size int64) (*store.QuotaExceeded, []store.QuotaWarning, error) {
	if h.db == nil {
		return nil, nil, nil
	}
	scopes := []store.QuotaScope{{Type: "repo", Value: name}}
	if username != "" {
		scopes = append(scopes, store.QuotaScope{Type: "user", Value: username})
	}
	return h.db.ReserveQuota(size, scopes...)
}

func (h *Handler) publishQuotaWarnings(warnings []store.QuotaWarning, actor string) {
	if h.hub == nil {
		return
	}
	for _, w := range warnings {
		h.hub.Publish(events.Event{
			Type:  "quota_warning",
			Name:  w.ScopeType + ":" + w.ScopeValue,
			Tag:   fmt.Sprintf("%d%%", w.Percent),
			Actor: actor,
		})
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// requestActor names the authenticated principal, if the auth middleware ran.
func requestActor(r *http.Request) string {
	if p, ok := auth.PrincipalFromRequest(r); ok {
		return p.Username
	}
	return ""
}

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
	_ = json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]string{{"code": code, "message": message}},
	})
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}

// IsV2Path reports whether a path belongs to the V2 protocol (used by the middleware).
func IsV2Path(path string) bool {
	return path == "/v2" || strings.HasPrefix(path, "/v2/")
}
