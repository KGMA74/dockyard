package auth

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"dockyard/internal/store"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
)

// Docker token auth (distribution "token" scheme): an unauthenticated request
// to /v2/* is answered with a Bearer challenge pointing at /v2/token; the
// client (docker login/push/pull) then trades its Basic credentials for a JWT
// there and retries. See https://distribution.github.io/distribution/spec/auth/token/

const (
	// registryTokenTTL is short on purpose: docker re-requests a token
	// whenever it gets a 401, so nothing long-lived is needed.
	registryTokenTTL = 5 * time.Minute

	// v2Service is the service name announced in challenges and expected in
	// token requests.
	v2Service = "dockyard-registry"

	anonymousUser = "anonymous"
)

var (
	reV2Manifests = regexp.MustCompile(`^/v2/(.+)/manifests/[^/]+$`)
	reV2Blobs     = regexp.MustCompile(`^/v2/(.+)/blobs/(?:uploads/.*|sha256:[a-f0-9]+)$`)
	reV2Tags      = regexp.MustCompile(`^/v2/(.+)/tags/list$`)
)

// v2Scope resolves the action and repository a V2 request needs.
func v2Scope(r *http.Request) (Action, string) {
	path := r.URL.Path
	var repo string
	switch {
	case reV2Manifests.MatchString(path):
		repo = reV2Manifests.FindStringSubmatch(path)[1]
	case reV2Blobs.MatchString(path):
		repo = reV2Blobs.FindStringSubmatch(path)[1]
	case reV2Tags.MatchString(path):
		repo = reV2Tags.FindStringSubmatch(path)[1]
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		return ActionPull, repo
	case http.MethodDelete:
		return ActionDelete, repo
	default: // POST, PUT, PATCH — uploads and manifest pushes
		return ActionPush, repo
	}
}

// IssueRegistryToken signs a short-lived token for the V2 protocol carrying
// the same role/repos claims the admin API uses.
func (m *Manager) IssueRegistryToken(username, role string, repoPatterns []string) (string, error) {
	claims := jwt.MapClaims{
		"sub":   username,
		"role":  role,
		"repos": repoPatterns,
		"exp":   time.Now().Add(registryTokenTTL).Unix(),
		"iat":   time.Now().Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secrets[0])
}

// V2Token — GET /v2/token. Docker sends Basic credentials plus
// service/scope query params. With V2_ANONYMOUS_PULL, requests without
// credentials get a pull-only token.
func (m *Manager) V2Token(anonymousPull bool) echo.HandlerFunc {
	return func(c echo.Context) error {
		username, password, hasBasic := c.Request().BasicAuth()

		var subject, role string
		var patterns []string
		switch {
		case hasBasic:
			u, ok := m.verify(username, password)
			if !ok {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			}
			subject, role, patterns = u.Username, u.Role, u.RepoPatterns
		case anonymousPull:
			// Anonymous tokens can only ever pull: role reader.
			subject, role = anonymousUser, store.RoleReader
		default:
			c.Response().Header().Set("WWW-Authenticate", `Basic realm="Dockyard Registry"`)
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		}

		tok, err := m.IssueRegistryToken(subject, role, patterns)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "token generation failed"})
		}
		return c.JSON(http.StatusOK, map[string]any{
			"token":        tok,
			"access_token": tok, // some clients read this key instead
			"expires_in":   int(registryTokenTTL.Seconds()),
			"issued_at":    time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// V2Middleware guards /v2/* with Bearer (token auth) or Basic, challenging
// unauthenticated clients toward /v2/token. With anonymousPull, pulls pass
// without credentials.
func (m *Manager) V2Middleware(anonymousPull bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			action, repo := v2Scope(r)

			var p Principal
			authenticated := false
			header := r.Header.Get("Authorization")
			switch {
			case strings.HasPrefix(header, "Bearer "):
				var err error
				p, err = m.validate(strings.TrimPrefix(header, "Bearer "))
				if err != nil {
					m.challenge(w, r, repo, action, "invalid token")
					return
				}
				authenticated = true
			case strings.HasPrefix(header, "Basic "):
				username, password, _ := r.BasicAuth()
				u, ok := m.verify(username, password)
				if !ok {
					m.challenge(w, r, repo, action, "invalid credentials")
					return
				}
				p = Principal{Username: u.Username, Role: u.Role, RepoPatterns: u.RepoPatterns}
				authenticated = true
			}

			if !authenticated {
				if anonymousPull && action == ActionPull {
					next.ServeHTTP(w, r)
					return
				}
				m.challenge(w, r, repo, action, "authentication required")
				return
			}
			if !Authorize(p, action, repo) {
				writeV2Error(w, http.StatusForbidden, "DENIED",
					fmt.Sprintf("%s access to %s denied for role %s", action, repo, p.Role))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// challenge answers 401 with both token-auth and Basic challenges so plain
// `docker login` and token-aware clients both work.
func (m *Manager) challenge(w http.ResponseWriter, r *http.Request, repo string, action Action, msg string) {
	realm := fmt.Sprintf("%s://%s/v2/token", requestScheme(r), r.Host)
	bearer := fmt.Sprintf("Bearer realm=%q,service=%q", realm, v2Service)
	if repo != "" {
		scope := "pull"
		if action != ActionPull {
			scope = "pull,push"
		}
		bearer += fmt.Sprintf(",scope=%q", fmt.Sprintf("repository:%s:%s", repo, scope))
	}
	w.Header().Add("WWW-Authenticate", bearer)
	w.Header().Add("WWW-Authenticate", `Basic realm="Dockyard Registry"`)
	writeV2Error(w, http.StatusUnauthorized, "UNAUTHORIZED", msg)
}

func requestScheme(r *http.Request) string {
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func writeV2Error(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"errors":[{"code":%q,"message":%q}]}`, code, message)
}
