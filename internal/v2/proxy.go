package v2

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// ProxyHandler transfère toutes les requêtes /v2/* vers une registry externe.
// Utilisé en mode "proxy" quand REGISTRY_URL est défini.
type ProxyHandler struct {
	rp *httputil.ReverseProxy
}

func NewProxy(targetURL, username, password string) (*ProxyHandler, error) {
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
	return &ProxyHandler{rp: rp}, nil
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
	h.rp.ServeHTTP(w, r)
}
