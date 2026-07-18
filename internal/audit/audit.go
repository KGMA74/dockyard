// Package audit records sensitive actions (logins, pushes, deletions, GC…)
// into the SQLite audit_log table and serves them back on /api/admin/audit.
package audit

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"dockyard/internal/auth"
	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
)

type Recorder struct {
	store *store.Store
}

func New(st *store.Store) *Recorder { return &Recorder{store: st} }

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

var reAuditManifest = regexp.MustCompile(`^/v2/(.+)/manifests/([^/]+)$`)

// V2Wrapper audits manifest pushes and deletions on the V2 protocol. Blob
// uploads are deliberately skipped (several requests per layer — pure noise);
// the manifest PUT that concludes a push is the meaningful event.
func (r *Recorder) V2Wrapper() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			m := reAuditManifest.FindStringSubmatch(req.URL.Path)
			if m == nil || (req.Method != http.MethodPut && req.Method != http.MethodDelete) {
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
			repo, ref := m[1], m[2]
			tag := ref
			if strings.HasPrefix(ref, "sha256:") {
				tag = ""
			}
			r.Record(actor, action, repo, tag, clientIP(req), strconv.Itoa(sw.status), ref)
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

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return xff
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i > 0 {
		host = host[:i]
	}
	return host
}
