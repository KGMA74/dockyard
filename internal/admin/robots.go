package admin

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"dockyard/internal/auth"
	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
)

// RobotsHandler manages robot accounts on /api/admin/robots (admin only).
// The secret is returned once, on creation or rotation, and never again.
type RobotsHandler struct {
	store *store.Store
}

func NewRobots(st *store.Store) *RobotsHandler { return &RobotsHandler{store: st} }

var robotName = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

// maxRobotLifetime caps expires_in_days; "never" is spelled 0.
const maxRobotLifetime = 3650

type robotWithSecret struct {
	*store.Robot
	Username string `json:"username"`
	Secret   string `json:"secret"`
}

type robotView struct {
	*store.Robot
	Username string `json:"username"`
}

func view(r *store.Robot) robotView { return robotView{Robot: r, Username: auth.RobotPrefix + r.Name} }

// expiry turns a lifetime in days into an expiry time; 0 means never.
func expiry(days int) (*time.Time, string) {
	if days < 0 || days > maxRobotLifetime {
		return nil, "expires_in_days must be between 0 (never) and " + strconv.Itoa(maxRobotLifetime)
	}
	if days == 0 {
		return nil, ""
	}
	t := time.Now().Add(time.Duration(days) * 24 * time.Hour)
	return &t, ""
}

// List — GET /api/admin/robots
func (h *RobotsHandler) List(c echo.Context) error {
	robots, err := h.store.ListRobots()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err500(err))
	}
	out := make([]robotView, len(robots))
	for i, r := range robots {
		out[i] = view(r)
	}
	return c.JSON(http.StatusOK, map[string]any{"robots": out, "count": len(out)})
}

// Create — POST /api/admin/robots
func (h *RobotsHandler) Create(c echo.Context) error {
	var body struct {
		Name          string   `json:"name"`
		Description   string   `json:"description"`
		Role          string   `json:"role"`
		RepoPatterns  []string `json:"repo_patterns"`
		ExpiresInDays int      `json:"expires_in_days"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	if len(body.Name) > 64 || !robotName.MatchString(body.Name) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name must be lowercase letters and digits, optionally separated by . _ or - (at most 64 characters)"})
	}
	if body.Role != store.RolePusher && body.Role != store.RoleReader {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "role must be pusher or reader"})
	}
	if len(body.Description) > 200 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "description must be at most 200 characters"})
	}
	if msg := patternsProblem(body.RepoPatterns); msg != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
	}
	expiresAt, msg := expiry(body.ExpiresInDays)
	if msg != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
	}
	if _, err := h.store.RobotByName(body.Name); err == nil {
		return c.JSON(http.StatusConflict, map[string]string{"error": "a robot account with this name already exists"})
	}
	secret, hash, err := auth.NewRobotSecret()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err500(err))
	}
	createdBy := ""
	if p, ok := auth.CurrentPrincipal(c); ok {
		createdBy = p.Username
	}
	r, err := h.store.CreateRobot(body.Name, body.Description, body.Role, body.RepoPatterns, hash, createdBy, expiresAt)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err500(err))
	}
	return c.JSON(http.StatusCreated, robotWithSecret{Robot: r, Username: auth.RobotPrefix + r.Name, Secret: secret})
}

// Rotate — POST /api/admin/robots/:id/secret. Issues a new secret; the old
// one stops working immediately.
func (h *RobotsHandler) Rotate(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid robot id"})
	}
	var body struct {
		ExpiresInDays int `json:"expires_in_days"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	expiresAt, msg := expiry(body.ExpiresInDays)
	if msg != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
	}
	secret, hash, err := auth.NewRobotSecret()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err500(err))
	}
	if err := h.store.RotateRobotSecret(id, hash, expiresAt); err != nil {
		return robotError(c, err)
	}
	r, err := h.store.RobotByID(id)
	if err != nil {
		return robotError(c, err)
	}
	return c.JSON(http.StatusOK, robotWithSecret{Robot: r, Username: auth.RobotPrefix + r.Name, Secret: secret})
}

// Delete — DELETE /api/admin/robots/:id. Outstanding registry tokens die on
// their next request (validate re-reads the robot).
func (h *RobotsHandler) Delete(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid robot id"})
	}
	if err := h.store.DeleteRobot(id); err != nil {
		return robotError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "robot account deleted"})
}

func robotError(c echo.Context, err error) error {
	if errors.Is(err, store.ErrRobotNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusInternalServerError, err500(err))
}
