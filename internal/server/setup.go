package server

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// SetupStep is one item of the getting-started checklist the console shows
// admins on a new install, ticked from the server's actual state.
type SetupStep struct {
	ID   string `json:"id"`
	Done bool   `json:"done"`
}

// handleSetup — GET /api/admin/setup (admin only). Steps that do not apply
// to this instance (no storage of its own in proxy mode, scanning off) are
// left out rather than shown as impossible to-dos.
func (s *Server) handleSetup(c echo.Context) error {
	var steps []SetupStep
	add := func(id string, done bool) { steps = append(steps, SetupStep{ID: id, Done: done}) }

	// The password the deployment set is in its config, often a placeholder
	// ("changeme"); still accepted means nobody has changed it.
	add("admin_password", !s.auth.StillUsesPassword(s.authUsername, s.authPassword))

	if s.mode != modeProxy && s.backend != nil {
		// The cached stats answer for any registry with content; an empty one
		// may only look empty because the cache predates the first push, and
		// listing an empty registry is cheap.
		hasImage := false
		if s.stats != nil {
			if st, err := s.stats.Get(); err == nil {
				hasImage = st.RepoCount > 0
			}
		}
		if !hasImage {
			repos, err := s.backend.ListRepositories()
			hasImage = err == nil && len(repos) > 0
		}
		add("first_image", hasImage)
	}
	if robots, err := s.store.ListRobots(); err == nil {
		add("robot_account", len(robots) > 0)
	}
	if s.scanEnabled {
		policy, err := s.store.GetVulnerabilityPolicy()
		add("scan_on_push", err == nil && policy.ScanOnPush)
	}
	// A team: more than the bootstrap admin, or sign-in through SSO.
	issuer, _ := s.auth.OIDCSummary()
	users, _ := s.store.CountUsers()
	add("team", users > 1 || (issuer != "" && issuer != "off"))
	// HTTPS served by Dockyard itself, or by a proxy in front (Echo reads
	// X-Forwarded-Proto). Display only: nothing here grants anything.
	add("https", s.tlsEnabled || c.Scheme() == "https")

	return c.JSON(http.StatusOK, map[string]any{"steps": steps})
}
