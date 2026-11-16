// Package audit records sensitive actions (logins, pushes, deletions, GC…)
// into the SQLite audit_log table and serves them back on /api/admin/audit.
package audit

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"dockyard/internal/auth"
	"dockyard/internal/reference"
	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
)

type Recorder struct {
	store *store.Store
	// ipOf resolves the client address of a plain net/http request (the V2
	// engine is not echo-based). Defaults to the socket address.
	ipOf func(*http.Request) string
}

func New(st *store.Store) *Recorder { return &Recorder{store: st, ipOf: remoteIP} }

// SetIPExtractor makes V2 audit entries use the same trusted-proxy-aware
// client IP as the rest of the server (see server.clientIPExtractor).
func (r *Recorder) SetIPExtractor(fn func(*http.Request) string) {
	if fn != nil {
		r.ipOf = fn
	}
}

// Record satisfies auth.AuditSink. Failures are deliberately swallowed: audit
// must never break the operation being audited.
func (r *Recorder) Record(actor, action, repo, tag, ip, result, details string) {
	_ = r.store.AddAudit(store.AuditEntry{
		Actor: actor, Action: action, Repo: repo, Tag: tag,
		SourceIP: ip, Result: result, Details: details,
	})
}

// AdminMiddleware audits every mutating /api/admin request (POST/PUT/DELETE)
// after it ran, with the actor from the JWT principal and the resulting
// status. Reads are not recorded.
func (r *Recorder) AdminMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			err := next(c)
			method := c.Request().Method
			if method == http.MethodGet || method == http.MethodHead {
				return err
			}
			// Auth endpoints are audited by the auth manager itself with
			// richer labels (login/logout/change-password) — skip them here.
			if strings.HasPrefix(c.Request().URL.Path, "/api/admin/auth/") {
				return err
			}
			actor := ""
			if p, ok := auth.CurrentPrincipal(c); ok {
				actor = p.Username
			}
			r.Record(
				actor,
				method+" "+c.Request().URL.Path,
				c.QueryParam("name"),
				"",
				c.RealIP(),
				strconv.Itoa(c.Response().Status),
				c.QueryParam("digest"),
			)
			return err
		}
	}
}

// V2Wrapper audits manifest pushes and deletions on the V2 protocol. Blob
// uploads are deliberately skipped (several requests per layer — pure noise);
// the manifest PUT that concludes a push is the meaningful event.
func (r *Recorder) V2Wrapper() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			route := reference.ParseV2Path(req.URL.Path)
			if route.Route != reference.RouteManifest || (req.Method != http.MethodPut && req.Method != http.MethodDelete) {
				next.ServeHTTP(w, req)
				return
			}
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, req)

			actor := "anonymous"
			if p, ok := auth.PrincipalFromRequest(req); ok {
				actor = p.Username
			}
			action := "push"
			if req.Method == http.MethodDelete {
				action = "delete-manifest"
			}
			repo, ref := route.Name, route.Ref
			tag := ref
			if reference.IsDigest(ref) {
				tag = ""
			}
			r.Record(actor, action, repo, tag, r.ipOf(req), strconv.Itoa(sw.status), ref)
		})
	}
}

// List — GET /api/admin/audit?repo=&actor=&limit=&offset= (admin only)
func (r *Recorder) List(c echo.Context) error {
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	offset, _ := strconv.Atoi(c.QueryParam("offset"))
	entries, total, err := r.store.ListAudit(c.QueryParam("repo"), c.QueryParam("actor"), limit, offset)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if entries == nil {
		entries = []*store.AuditEntry{}
	}
	return c.JSON(http.StatusOK, map[string]any{"entries": entries, "total": total})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// remoteIP is the socket peer address. X-Forwarded-For is deliberately not
// consulted here: it is client-controlled, so an audit trail built on it can
// be made to name any address. Trusted-proxy handling is injected through
// SetIPExtractor.
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
