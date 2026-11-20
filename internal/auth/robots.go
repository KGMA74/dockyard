package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

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
	// it scans. Its password is random per process and never stored.
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

// ScannerCredentials are what the scan dispatcher hands trivy. Pulls made
// with them are exempt from the vulnerability gate — a blocked image must
// still be re-scannable.
func (m *Manager) ScannerCredentials() (username, password string) {
	return ScannerUsername, m.scannerSecret
}

func newScannerSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// authenticate checks registry credentials: the internal scanner, a robot
// account or a user. Only the registry protocol calls it; the console login
// accepts users only.
func (m *Manager) authenticate(username, password string) (Principal, bool) {
	if username == ScannerUsername && m.scannerSecret != "" {
		if subtle.ConstantTimeCompare([]byte(password), []byte(m.scannerSecret)) == 1 {
			return Principal{Username: ScannerUsername, Role: store.RoleReader, Scanner: true}, true
		}
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
