package v2

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"dockyard/internal/auth"
	"dockyard/internal/events"
	"dockyard/internal/reference"
)

// ProxyHandler transfère toutes les requêtes /v2/* vers une registry externe.
// Utilisé en mode "proxy" quand REGISTRY_URL est défini.
type ProxyHandler struct {
	rp  *httputil.ReverseProxy
	hub *events.Hub
}

func NewProxy(targetURL, username, password string, hub *events.Hub) (*ProxyHandler, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(req *httputil.ProxyRequest) {
			req.SetURL(target)
			req.SetXForwarded()
			if username != "" {
				req.Out.SetBasicAuth(username, password)
			}
		},
	}
	return &ProxyHandler{rp: rp, hub: hub}, nil
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	start := time.Now()

	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
	route := reference.ParseV2Path(r.URL.Path)

	// Refuse malformed references here instead of forwarding them: the
	// upstream may normalise "a/../b" into another repository than the one
	// the auth middleware authorized.
	if err := route.Validate(); err != nil {
		registryError(rec, http.StatusBadRequest, reference.V2ErrorCode(err), err.Error())
		return
	}
	// The upstream catalog cannot be filtered per user — a principal limited
	// to some repositories does not get to enumerate all of them.
	if route.Route == reference.RouteCatalog {
		if p, ok := auth.PrincipalFromRequest(r); ok && len(p.RepoPatterns) > 0 {
			registryError(rec, http.StatusForbidden, "DENIED", "catalog listing is not available to accounts restricted to specific repositories")
			return
		}
	}

	h.rp.ServeHTTP(rec, r)

	// Same tag-only rule as the embedded handler: skip digest-referenced PUTs
	// (platform manifests in a multi-arch push) to avoid repeat notifications.
	if h.hub != nil && r.Method == http.MethodPut && rec.status < 300 &&
		route.Route == reference.RouteManifest && !reference.IsDigest(route.Ref) {
		h.hub.Publish(events.Event{Type: "push", Name: route.Name, Tag: route.Ref, Actor: requestActor(r)})
	}

	slog.Info("v2 proxy",
		"method", r.Method,
		"path", r.URL.Path,
		"status", rec.status,
		"duration_ms", time.Since(start).Milliseconds(),
	)
}
