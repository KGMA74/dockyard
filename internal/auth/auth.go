package auth

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dockyard/internal/store"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

// principalKey is the echo context key holding the authenticated Principal.
const principalKey = "auth.principal"

// Principal is the authenticated identity extracted from a verified JWT.
type Principal struct {
	Username     string
	Role         string
	RepoPatterns []string
}

// Action is a permission checked by Authorize.
type Action string

const (
	ActionPull   Action = "pull"
	ActionPush   Action = "push"
	ActionDelete Action = "delete"
	ActionAdmin  Action = "admin"
)

type Manager struct {
	users     *store.Store
	username  string // bootstrap admin username (also used by /v2 basic auth)
	secret    []byte
	blacklist map[string]time.Time // token → expiration
	mu        sync.Mutex
}

// New builds the auth manager on top of the SQLite user store. On first boot
// with an empty users table, the legacy single admin is migrated: the bcrypt
// hash persisted at <dataDir>/auth/password.bcrypt wins over initialPassword,
// so a password changed at runtime survives the upgrade.
func New(username, initialPassword, jwtSecret, dataDir string, users *store.Store) (*Manager, error) {
	m := &Manager{
		users:     users,
		username:  username,
		secret:    []byte(jwtSecret),
		blacklist: make(map[string]time.Time),
	}
	count, err := users.CountUsers()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		hash, err := legacyOrInitialHash(filepath.Join(dataDir, "auth", "password.bcrypt"), initialPassword)
		if err != nil {
			return nil, err
		}
		if _, err := users.CreateUser(username, hash, store.RoleAdmin, nil); err != nil {
			return nil, err
		}
	}
	go m.cleanupLoop()
	return m, nil
}

func legacyOrInitialHash(hashFile, initialPassword string) (string, error) {
	if raw, err := os.ReadFile(hashFile); err == nil && len(raw) > 0 {
		return string(raw), nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(initialPassword), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// cleanupLoop supprime de la blacklist les tokens naturellement expirés.
func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		m.mu.Lock()
		for tok, exp := range m.blacklist {
			if now.After(exp) {
				delete(m.blacklist, tok)
			}
		}
		m.mu.Unlock()
	}
}

func (m *Manager) verify(username, password string) (*store.User, bool) {
	u, err := m.users.GetUser(username)
	if err != nil {
		return nil, false
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, false
	}
	return u, true
}

func (m *Manager) token(u *store.User) (string, error) {
	claims := jwt.MapClaims{
		"sub":   u.Username,
		"role":  u.Role,
		"repos": u.RepoPatterns,
		"exp":   time.Now().Add(24 * time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

func (m *Manager) parseToken(raw string) (*jwt.Token, error) {
	return jwt.Parse(raw, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return m.secret, nil
	})
}

// validate checks blacklist + signature and returns the token's Principal.
func (m *Manager) validate(raw string) (Principal, error) {
	m.mu.Lock()
	_, blacklisted := m.blacklist[raw]
	m.mu.Unlock()
	if blacklisted {
		return Principal{}, errors.New("token has been revoked")
	}
	tok, err := m.parseToken(raw)
	if err != nil || !tok.Valid {
		return Principal{}, errors.New("invalid token")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return Principal{}, errors.New("invalid claims")
	}
	p := Principal{}
	p.Username, _ = claims["sub"].(string)
	p.Role, _ = claims["role"].(string)
	if p.Role == "" {
		// Token issued before the RBAC migration — the only account then was
		// the admin, keep it working until it expires (24h at most).
		p.Role = store.RoleAdmin
	}
	if rawRepos, ok := claims["repos"].([]any); ok {
		for _, r := range rawRepos {
			if s, ok := r.(string); ok {
				p.RepoPatterns = append(p.RepoPatterns, s)
			}
		}
	}
	return p, nil
}

// Authorize reports whether p may perform action on repo. Repo patterns
// restrict pull/push/delete; an empty pattern list means all repositories.
// '*' in a pattern matches any characters, including '/'.
func Authorize(p Principal, action Action, repo string) bool {
	switch action {
	case ActionAdmin:
		return p.Role == store.RoleAdmin
	case ActionDelete:
		if p.Role != store.RoleAdmin {
			return false
		}
	case ActionPush:
		if p.Role != store.RoleAdmin && p.Role != store.RolePusher {
			return false
		}
	case ActionPull:
		// every role can pull
	default:
		return false
	}
	return MatchesRepo(p.RepoPatterns, repo)
}

// MatchesRepo reports whether repo matches at least one pattern. No patterns
// means no restriction. Only '*' is special and matches any run of characters.
func MatchesRepo(patterns []string, repo string) bool {
	if len(patterns) == 0 || repo == "" {
		return true
	}
	for _, pattern := range patterns {
		if wildcardMatch(pattern, repo) {
			return true
		}
	}
	return false
}

func wildcardMatch(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for _, part := range parts[1 : len(parts)-1] {
		idx := strings.Index(s, part)
		if idx < 0 {
			return false
		}
		s = s[idx+len(part):]
	}
	return strings.HasSuffix(s, parts[len(parts)-1])
}

// CurrentPrincipal returns the Principal set by Middleware, if any.
func CurrentPrincipal(c echo.Context) (Principal, bool) {
	p, ok := c.Get(principalKey).(Principal)
	return p, ok
}

// RequireAdmin is a route-level middleware rejecting non-admin principals.
func RequireAdmin(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		p, ok := CurrentPrincipal(c)
		if !ok || !Authorize(p, ActionAdmin, "") {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "admin role required"})
		}
		return next(c)
	}
}

// Login — POST /api/admin/auth/login
func (m *Manager) Login(c echo.Context) error {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	u, ok := m.verify(body.Username, body.Password)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
	}
	tok, err := m.token(u)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "token generation failed"})
	}
	return c.JSON(http.StatusOK, map[string]string{"token": tok, "role": u.Role})
}

// Logout — POST /api/admin/auth/logout (JWT requis)
func (m *Manager) Logout(c echo.Context) error {
	raw := strings.TrimPrefix(c.Request().Header.Get("Authorization"), "Bearer ")
	tok, err := m.parseToken(raw)
	if err != nil || !tok.Valid {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid token"})
	}
	exp, err := tok.Claims.GetExpirationTime()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "cannot read token expiry"})
	}
	m.mu.Lock()
	m.blacklist[raw] = exp.Time
	m.mu.Unlock()
	return c.JSON(http.StatusOK, map[string]string{"message": "logged out"})
}

// ChangePassword — POST /api/admin/auth/password (JWT requis). Changes the
// password of the authenticated user.
func (m *Manager) ChangePassword(c echo.Context) error {
	p, ok := CurrentPrincipal(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
	}
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	if _, ok := m.verify(p.Username, body.CurrentPassword); !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "wrong current password"})
	}
	if len(body.NewPassword) < 8 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to hash password"})
	}
	if err := m.users.UpdateUserPassword(p.Username, string(hash)); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update password"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "password updated"})
}

func (m *Manager) Username() string { return m.username }

// PasswordHash returns the bootstrap admin's bcrypt hash (used by the /v2
// basic-auth wrapper until Docker token auth lands).
func (m *Manager) PasswordHash() (string, error) {
	u, err := m.users.GetUser(m.username)
	if err != nil {
		return "", err
	}
	return u.PasswordHash, nil
}

// BasicAuthMiddleware protects /v2/* routes with HTTP Basic Auth + bcrypt.
// Enabled only when V2_AUTH_ENABLED=true; otherwise /v2/* is open.
func BasicAuthMiddleware(username, passwordHash string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok {
				w.Header().Set("WWW-Authenticate", `Basic realm="Dockyard Registry"`)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"errors":[{"code":"UNAUTHORIZED","message":"authentication required"}]}`))
				return
			}
			if user != username {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(pass)); err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Middleware vérifie le JWT, rejette les tokens blacklistés et installe le
// Principal dans le contexte.
func (m *Manager) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			header := c.Request().Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing or invalid token"})
			}
			p, err := m.validate(strings.TrimPrefix(header, "Bearer "))
			if err != nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": err.Error()})
			}
			c.Set(principalKey, p)
			return next(c)
		}
	}
}

// MiddlewareEventStream validates a JWT the same way as Middleware, but also
// accepts it via a ?token= query param. EventSource (used for the SSE push
// feed) can't set an Authorization header, so it has no other way to
// authenticate.
func (m *Manager) MiddlewareEventStream() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			raw := strings.TrimPrefix(c.Request().Header.Get("Authorization"), "Bearer ")
			if raw == "" {
				raw = c.QueryParam("token")
			}
			if raw == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing token"})
			}
			p, err := m.validate(raw)
			if err != nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": err.Error()})
			}
			c.Set(principalKey, p)
			return next(c)
		}
	}
}
