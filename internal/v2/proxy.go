package v2

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"dockyard/internal/events"
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
	h.rp.ServeHTTP(rec, r)

	// Same tag-only rule as the embedded handler: skip digest-referenced PUTs
	// (platform manifests in a multi-arch push) to avoid repeat notifications.
	if h.hub != nil && r.Method == http.MethodPut && rec.status < 300 {
		if m := reManifests.FindStringSubmatch(r.URL.Path); m != nil && !strings.HasPrefix(m[2], "sha256:") {
			h.hub.Publish(events.Event{Type: "push", Name: m[1], Tag: m[2]})
		}
	}

	slog.Info("v2 proxy",
		"method", r.Method,
		"path", r.URL.Path,
		"status", rec.status,
		"duration_ms", time.Since(start).Milliseconds(),
	)
}
