package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"

	"dockyard/internal/reference"
	"dockyard/internal/store"
)

// Robot accounts log in to the registry as "robot+<name>" with a generated
// secret. The prefix can never collide with a user: '+' is not allowed in
// usernames.
const (
	RobotPrefix = "robot+"
	// robotSecretPrefix makes leaked secrets recognisable to secret scanners
	// (and to people reading a CI log).
	robotSecretPrefix = "dkr_"

	// ScannerUsername is the internal identity trivy uses to pull the images
	// it scans. It has no password: it exists only in the tokens
	// IssueScannerToken signs, one repository at a time.
	ScannerUsername = "dockyard-scanner"
)

// NewRobotSecret returns a fresh robot secret and the hash to store.
func NewRobotSecret() (secret, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	secret = robotSecretPrefix + base64.RawURLEncoding.EncodeToString(b)
	return secret, hashToken(secret), nil
}

// IssueScannerToken signs the registry token the scan dispatcher hands trivy
// for one scan: the internal scanner identity, pull only, on repo alone, for
// ttl. It is signed and audienced like the tokens /v2/token issues, so the
// admin API refuses it; nothing stores it, and only this server can mint
// one. Pulls made with it skip the vulnerability gate, so a blocked image
// can still be scanned again.
func (m *Manager) IssueScannerToken(repo string, ttl time.Duration) (string, error) {
	if !reference.ValidName(repo) {
		return "", fmt.Errorf("scanner token: %w %q", reference.ErrInvalidName, repo)
	}
	if ttl <= 0 {
		return "", errors.New("scanner token: lifetime must be positive")
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"aud":   audRegistry,
		"sub":   ScannerUsername,
		"scn":   true,
		"role":  store.RoleReader,
		"repos": []string{repo},
		"iat":   now.Unix(),
		"exp":   now.Add(ttl).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secrets[0])
}

// scannerPrincipal checks the claims of a scanner token and returns the
// identity it grants: a reader confined to the one repository it names. A
// token naming no repository, several, or a pattern is not one this server
// signed and is refused.
func scannerPrincipal(claims jwt.MapClaims, sub string, registry bool) (Principal, error) {
	repos, _ := claims["repos"].([]any)
	if sub != ScannerUsername || !registry || len(repos) != 1 {
		return Principal{}, errors.New("invalid claims")
	}
	repo, _ := repos[0].(string)
	if !reference.ValidName(repo) { // also rules out '*' patterns
		return Principal{}, errors.New("invalid claims")
	}
	return Principal{
		Username:     ScannerUsername,
		Role:         store.RoleReader,
		RepoPatterns: []string{repo},
		Scanner:      true,
		Registry:     true,
	}, nil
}

// authenticate checks registry credentials: a robot account or a user. Only
// the registry protocol calls it; the console login accepts users only.
func (m *Manager) authenticate(username, password string) (Principal, bool) {
	if username == ScannerUsername {
		// The scanner has no password, only per-scan tokens.
		return Principal{}, false
	}
	if name, ok := strings.CutPrefix(username, RobotPrefix); ok {
		return m.authenticateRobot(name, password)
	}
	u, ok := m.verify(username, password)
	if !ok {
		// The account's CLI secret works wherever its password does on the
		// registry — and is the only way in for an SSO account.
		u, ok = m.verifyCLISecret(username, password)
		if !ok {
			return Principal{}, false
		}
	}
	return Principal{Username: u.Username, Role: u.Role, RepoPatterns: u.RepoPatterns}, true
}

func (m *Manager) verifyCLISecret(username, secret string) (*store.User, bool) {
	if !strings.HasPrefix(secret, cliSecretPrefix) {
		return nil, false
	}
	u, err := m.users.GetUser(username)
	if err != nil || u.CLISecretHash == "" {
		return nil, false
	}
	if subtle.ConstantTimeCompare([]byte(hashToken(secret)), []byte(u.CLISecretHash)) != 1 {
		return nil, false
	}
	return u, true
}

// cliSecretPrefix marks personal CLI secrets, as dkr_ marks robot ones.
const cliSecretPrefix = "dkc_"

// Me — GET /api/admin/auth/me: the signed-in account as the server sees it.
func (m *Manager) Me(c echo.Context) error {
	p, ok := CurrentPrincipal(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
	}
	u, err := m.users.GetUser(p.Username)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "account no longer exists"})
	}
	return c.JSON(http.StatusOK, u)
}

// CreateCLISecret — POST /api/admin/auth/cli-secret: issues (or replaces) the
// caller's CLI secret, returned once.
func (m *Manager) CreateCLISecret(c echo.Context) error {
	p, ok := CurrentPrincipal(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "secret generation failed"})
	}
	secret := cliSecretPrefix + base64.RawURLEncoding.EncodeToString(b)
	if err := m.users.SetCLISecret(p.Username, hashToken(secret)); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	m.auditRecord(p.Username, "cli-secret", c.RealIP(), "success", "issued")
	return c.JSON(http.StatusCreated, map[string]string{"username": p.Username, "secret": secret})
}

// DeleteCLISecret — DELETE /api/admin/auth/cli-secret
func (m *Manager) DeleteCLISecret(c echo.Context) error {
	p, ok := CurrentPrincipal(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
	}
	if err := m.users.SetCLISecret(p.Username, ""); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	m.auditRecord(p.Username, "cli-secret", c.RealIP(), "success", "revoked")
	return c.JSON(http.StatusOK, map[string]string{"message": "CLI secret revoked"})
}

func (m *Manager) authenticateRobot(name, secret string) (Principal, bool) {
	r, err := m.users.RobotByName(name)
	if err != nil {
		return Principal{}, false
	}
	if subtle.ConstantTimeCompare([]byte(hashToken(secret)), []byte(r.SecretHash)) != 1 || r.Expired(time.Now()) {
		return Principal{}, false
	}
	_ = m.users.TouchRobot(r.ID)
	return Principal{Username: RobotPrefix + r.Name, Role: r.Role, RepoPatterns: r.RepoPatterns, RobotID: r.ID}, true
}

// robotPrincipal re-reads a robot named in a registry token, so deleting it,
// rotating its secret's expiry or changing nothing at all is enforced on the
// next request, not when the token expires.
func (m *Manager) robotPrincipal(id int64) (Principal, bool) {
	r, err := m.users.RobotByID(id)
	if err != nil || r.Expired(time.Now()) {
		return Principal{}, false
	}
	return Principal{Username: RobotPrefix + r.Name, Role: r.Role, RepoPatterns: r.RepoPatterns, RobotID: r.ID}, true
}

// IsScannerRequest reports whether a registry request comes from Dockyard's
// own scanner. With registry auth on, that is the authenticated principal;
// with it off nobody is authenticated, and the scanner — which always pulls
// through localhost — is recognised by its loopback address instead, unless
// the request went through a reverse proxy (which would make every client
// look local).
func IsScannerRequest(r *http.Request) bool {
	if p, ok := PrincipalFromRequest(r); ok {
		return p.Scanner
	}
	for _, header := range []string{"X-Forwarded-For", "X-Real-Ip", "Forwarded"} {
		if r.Header.Get(header) != "" {
			return false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
