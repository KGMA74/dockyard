package scan

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"dockyard/internal/auth"
	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
)

// PolicyHandler exposes the vulnerability policy on
// /api/admin/vulnerability-policy (admin only). It is registered even with
// scanning off, so the page can say why nothing is enforced.
type PolicyHandler struct {
	store           *store.Store
	policy          *PolicyState
	scanningEnabled bool
}

func NewPolicyHandler(st *store.Store, policy *PolicyState, scanningEnabled bool) *PolicyHandler {
	return &PolicyHandler{store: st, policy: policy, scanningEnabled: scanningEnabled}
}

// CVE-2024-1234, GHSA-xxxx-xxxx-xxxx, RUSTSEC-2023-0001, DLA-1234-1…
var vulnID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-[A-Za-z0-9][A-Za-z0-9._:-]{1,62}$`)

func actorOf(c echo.Context) string {
	if p, ok := auth.CurrentPrincipal(c); ok {
		return p.Username
	}
	return ""
}

// Get — GET /api/admin/vulnerability-policy
func (h *PolicyHandler) Get(c echo.Context) error {
	allowlist, err := h.store.ListAllowedCVEs()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	p := h.policy.Get()
	return c.JSON(http.StatusOK, map[string]any{
		"policy":           p,
		"allowlist":        allowlist,
		"scanning_enabled": h.scanningEnabled,
	})
}

// Update — PUT /api/admin/vulnerability-policy {scan_on_push, block_severity}
func (h *PolicyHandler) Update(c echo.Context) error {
	var body struct {
		ScanOnPush    bool   `json:"scan_on_push"`
		BlockSeverity string `json:"block_severity"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	body.BlockSeverity = strings.ToUpper(body.BlockSeverity)
	if !store.ValidBlockSeverity(body.BlockSeverity) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "block_severity must be CRITICAL, HIGH, MEDIUM, LOW or empty"})
	}
	p, err := h.policy.Update(body.ScanOnPush, body.BlockSeverity, actorOf(c))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, p)
}

// Allow — POST /api/admin/vulnerability-policy/allowlist
// {cve_id, reason, expires_in_days}
func (h *PolicyHandler) Allow(c echo.Context) error {
	var body struct {
		CVEID         string `json:"cve_id"`
		Reason        string `json:"reason"`
		ExpiresInDays int    `json:"expires_in_days"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	body.CVEID = strings.TrimSpace(body.CVEID)
	if !vulnID.MatchString(body.CVEID) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "not a vulnerability identifier (e.g. CVE-2024-1234 or GHSA-xxxx-xxxx-xxxx)"})
	}
	if len(body.Reason) > 500 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "reason must be at most 500 characters"})
	}
	if body.ExpiresInDays < 0 || body.ExpiresInDays > 3650 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "expires_in_days must be between 0 (never) and 3650"})
	}
	var expiresAt *time.Time
	if body.ExpiresInDays > 0 {
		t := time.Now().Add(time.Duration(body.ExpiresInDays) * 24 * time.Hour)
		expiresAt = &t
	}
	if err := h.store.AllowCVE(body.CVEID, strings.TrimSpace(body.Reason), actorOf(c), expiresAt); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, map[string]string{"cve_id": strings.ToUpper(body.CVEID)})
}

// Disallow — DELETE /api/admin/vulnerability-policy/allowlist/:cve
func (h *PolicyHandler) Disallow(c echo.Context) error {
	if err := h.store.DisallowCVE(c.Param("cve")); err != nil {
		if errors.Is(err, store.ErrAllowedCVENotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "removed from the allowlist"})
}
