package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dockyard/internal/store"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

const (
	// principalKey is the echo context key holding the authenticated Principal.
	principalKey = "auth.principal"

	// accessTTL is deliberately short: revoking a session only cuts refresh,
	// outstanding access tokens die on their own within this window.
	accessTTL  = 15 * time.Minute
	refreshTTL = 30 * 24 * time.Hour
)

// Principal is the authenticated identity extracted from a verified JWT.
type Principal struct {
	Username     string
	Role         string
	RepoPatterns []string
	SessionID    int64
}

// Action is a permission checked by Authorize.
type Action string

const (
	ActionPull   Action = "pull"
	ActionPush   Action = "push"
	ActionDelete Action = "delete"
	ActionAdmin  Action = "admin"
)

// AuditSink receives audit events. Implemented by audit.Recorder; declared
// here to avoid an import cycle.
type AuditSink interface {
	Record(actor, action, repo, tag, ip, result, details string)
}

type Manager struct {
	users    *store.Store
	username string // bootstrap admin username (also used by /v2 basic auth)
	// secrets[0] signs new tokens; the rest are still accepted for
	// verification (JWT_SECRET_PREVIOUS rotation grace window).
	secrets [][]byte
	audit   AuditSink
}

// SetAuditSink enables audit records for logins, logouts and password
// changes. Optional; nil disables.
func (m *Manager) SetAuditSink(sink AuditSink) { m.audit = sink }

func (m *Manager) auditRecord(actor, action, ip, result, details string) {
	if m.audit != nil {
		m.audit.Record(actor, action, "", "", ip, result, details)
	}
}

// New builds the auth manager on top of the SQLite user store. On first boot
// with an empty users table, the legacy single admin is migrated: the bcrypt
// hash persisted at <dataDir>/auth/password.bcrypt wins over initialPassword,
// so a password changed at runtime survives the upgrade.
func New(username, initialPassword, jwtSecret, previousSecret, dataDir string, users *store.Store) (*Manager, error) {
	m := &Manager{
		users:    users,
		username: username,
		secrets:  [][]byte{[]byte(jwtSecret)},
	}
	if previousSecret != "" {
		m.secrets = append(m.secrets, []byte(previousSecret))
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

// cleanupLoop prunes expired sessions and spent token revocations.
func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		_ = m.users.PruneExpired()
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

func (m *Manager) token(u *store.User, sessionID int64) (string, error) {
	claims := jwt.MapClaims{
		"sub":   u.Username,
		"role":  u.Role,
		"repos": u.RepoPatterns,
		"sid":   strconv.FormatInt(sessionID, 10),
		"exp":   time.Now().Add(accessTTL).Unix(),
		"iat":   time.Now().Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secrets[0])
}

// parseToken accepts tokens signed with the current secret or, during a
// rotation grace window, the previous one.
func (m *Manager) parseToken(raw string) (*jwt.Token, error) {
	var lastErr error
	for _, secret := range m.secrets {
		tok, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return secret, nil
		})
		if err == nil && tok.Valid {
			return tok, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("invalid token")
	}
	return nil, lastErr
}

func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func newRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// validate checks revocation + signature and returns the token's Principal.
func (m *Manager) validate(raw string) (Principal, error) {
	if revoked, err := m.users.IsTokenRevoked(hashToken(raw)); err == nil && revoked {
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
		// the admin, keep it working until it expires.
		p.Role = store.RoleAdmin
	}
	if sid, ok := claims["sid"].(string); ok {
		p.SessionID, _ = strconv.ParseInt(sid, 10, 64)
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

// issueSession creates a session and returns the login/refresh response body.
func (m *Manager) issueSession(c echo.Context, u *store.User) (map[string]any, error) {
	refresh, err := newRefreshToken()
	if err != nil {
		return nil, err
	}
	sid, err := m.users.CreateSession(
		u.ID, hashToken(refresh),
		c.Request().UserAgent(), c.RealIP(),
		time.Now().Add(refreshTTL),
	)
	if err != nil {
		return nil, err
	}
	access, err := m.token(u, sid)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"token":         access,
		"refresh_token": refresh,
		"role":          u.Role,
		"expires_in":    int(accessTTL.Seconds()),
	}, nil
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
		m.auditRecord(body.Username, "login", c.RealIP(), "failure", "invalid credentials")
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
	}
	resp, err := m.issueSession(c, u)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "session creation failed"})
	}
	m.auditRecord(u.Username, "login", c.RealIP(), "success", "")
	return c.JSON(http.StatusOK, resp)
}

// Refresh — POST /api/admin/auth/refresh. Authenticated by the refresh token
// itself; rotates it (single use) and returns a fresh access token.
func (m *Manager) Refresh(c echo.Context) error {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.Bind(&body); err != nil || body.RefreshToken == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "refresh_token required"})
	}
	sess, u, err := m.users.SessionByRefreshHash(hashToken(body.RefreshToken))
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid or expired refresh token"})
	}
	refresh, err := newRefreshToken()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "token generation failed"})
	}
	if err := m.users.RotateSessionRefresh(sess.ID, hashToken(refresh), time.Now().Add(refreshTTL)); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "session rotation failed"})
	}
	access, err := m.token(u, sess.ID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "token generation failed"})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"token":         access,
		"refresh_token": refresh,
		"role":          u.Role,
		"expires_in":    int(accessTTL.Seconds()),
	})
}

// Logout — POST /api/admin/auth/logout (JWT requis). Revokes the access token
// (persisted, survives restarts) and kills its session so the refresh token
// dies with it.
func (m *Manager) Logout(c echo.Context) error {
	raw := strings.TrimPrefix(c.Request().Header.Get("Authorization"), "Bearer ")
	p, err := m.validate(raw)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid token"})
	}
	tok, err := m.parseToken(raw)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid token"})
	}
	exp, err := tok.Claims.GetExpirationTime()
	if err != nil || exp == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "cannot read token expiry"})
	}
	if err := m.users.RevokeToken(hashToken(raw), exp.Time); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "revocation failed"})
	}
	if p.SessionID != 0 {
		_ = m.users.DeleteSession(p.SessionID)
	}
	m.auditRecord(p.Username, "logout", c.RealIP(), "success", "")
	return c.JSON(http.StatusOK, map[string]string{"message": "logged out"})
}

// ListSessions — GET /api/admin/sessions (admin only)
func (m *Manager) ListSessions(c echo.Context) error {
	sessions, err := m.users.ListSessions()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if sessions == nil {
		sessions = []*store.Session{}
	}
	current := int64(0)
	if p, ok := CurrentPrincipal(c); ok {
		current = p.SessionID
	}
	return c.JSON(http.StatusOK, map[string]any{
		"sessions":   sessions,
		"count":      len(sessions),
		"current_id": current,
	})
}

// RevokeSession — DELETE /api/admin/sessions/:id (admin only). Kills the
// refresh token; outstanding access tokens expire within accessTTL.
func (m *Manager) RevokeSession(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid session id"})
	}
	if err := m.users.DeleteSession(id); err != nil {
		if errors.Is(err, store.ErrSessionNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "session not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "session revoked"})
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
	m.auditRecord(p.Username, "change-password", c.RealIP(), "success", "")
	return c.JSON(http.StatusOK, map[string]string{"message": "password updated"})
}

func (m *Manager) Username() string { return m.username }

// Middleware vérifie le JWT, rejette les tokens révoqués et installe le
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
