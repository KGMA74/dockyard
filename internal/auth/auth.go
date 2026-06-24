package auth

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

type Manager struct {
	username  string
	secret    []byte
	hashFile  string
	blacklist map[string]time.Time // token → expiration
	mu        sync.Mutex
}

func New(username, initialPassword, jwtSecret, dataDir string) (*Manager, error) {
	m := &Manager{
		username:  username,
		secret:    []byte(jwtSecret),
		hashFile:  filepath.Join(dataDir, "auth", "password.bcrypt"),
		blacklist: make(map[string]time.Time),
	}
	if _, err := os.Stat(m.hashFile); os.IsNotExist(err) {
		if err := m.writeHash(initialPassword); err != nil {
			return nil, err
		}
	}
	go m.cleanupLoop()
	return m, nil
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

func (m *Manager) writeHash(password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.hashFile), 0700); err != nil {
		return err
	}
	return os.WriteFile(m.hashFile, hash, 0600)
}

func (m *Manager) verify(username, password string) bool {
	if username != m.username {
		return false
	}
	hash, err := os.ReadFile(m.hashFile)
	if err != nil {
		return false
	}
	return bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
}

func (m *Manager) token() (string, error) {
	claims := jwt.MapClaims{
		"sub": m.username,
		"exp": time.Now().Add(24 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
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

// Login — POST /api/admin/auth/login
func (m *Manager) Login(c echo.Context) error {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	if !m.verify(body.Username, body.Password) {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
	}
	tok, err := m.token()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "token generation failed"})
	}
	return c.JSON(http.StatusOK, map[string]string{"token": tok})
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

// ChangePassword — POST /api/admin/auth/password (JWT requis)
func (m *Manager) ChangePassword(c echo.Context) error {
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	if !m.verify(m.username, body.CurrentPassword) {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "wrong current password"})
	}
	if len(body.NewPassword) < 8 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
	}
	if err := m.writeHash(body.NewPassword); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update password"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "password updated"})
}

func (m *Manager) Username() string { return m.username }

func (m *Manager) PasswordHash() (string, error) {
	hash, err := os.ReadFile(m.hashFile)
	return string(hash), err
}

// BasicAuthMiddleware protects /v2/* routes with HTTP Basic Auth + bcrypt.
// Enabled only when V2_AUTH_ENABLED=true; otherwise /v2/* is open.
func BasicAuthMiddleware(username, passwordHash string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok {
				w.Header().Set("WWW-Authenticate", `Basic realm="Maestro Registry"`)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"errors":[{"code":"UNAUTHORIZED","message":"authentication required"}]}`))
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

// Middleware vérifie le JWT et rejette les tokens blacklistés.
func (m *Manager) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			header := c.Request().Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing or invalid token"})
			}
			raw := strings.TrimPrefix(header, "Bearer ")
			m.mu.Lock()
			_, blacklisted := m.blacklist[raw]
			m.mu.Unlock()
			if blacklisted {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "token has been revoked"})
			}
			tok, err := m.parseToken(raw)
			if err != nil || !tok.Valid {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			}
			return next(c)
		}
	}
}
