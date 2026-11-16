package admin

import (
	"errors"
	"net/http"
	"strings"

	"dockyard/internal/auth"
	"dockyard/internal/reference"
	"dockyard/internal/store"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

// UsersHandler exposes user management on /api/admin/users. It works in both
// embedded and proxy modes since users live in SQLite, not registry storage.
// All routes are admin-only (enforced by auth.RequireAdmin at registration).
type UsersHandler struct {
	store *store.Store
}

func NewUsers(st *store.Store) *UsersHandler { return &UsersHandler{store: st} }

// List — GET /api/admin/users
func (h *UsersHandler) List(c echo.Context) error {
	users, err := h.store.ListUsers()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err500(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"users": users, "count": len(users)})
}

// Create — POST /api/admin/users
func (h *UsersHandler) Create(c echo.Context) error {
	var body struct {
		Username     string   `json:"username"`
		Password     string   `json:"password"`
		Role         string   `json:"role"`
		RepoPatterns []string `json:"repo_patterns"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	if !store.ValidRole(body.Role) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "a valid role (admin, pusher, reader) is required"})
	}
	if msg := usernameProblem(body.Username); msg != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
	}
	if msg := patternsProblem(body.RepoPatterns); msg != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
	}
	if len(body.Password) < 8 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err500(err))
	}
	u, err := h.store.CreateUser(body.Username, string(hash), body.Role, body.RepoPatterns)
	if err != nil {
		return c.JSON(http.StatusConflict, err500(err))
	}
	return c.JSON(http.StatusCreated, u)
}

// Update — PUT /api/admin/users/:username (role, repo patterns and/or password)
func (h *UsersHandler) Update(c echo.Context) error {
	username := c.Param("username")
	var body struct {
		Role         *string  `json:"role"`
		RepoPatterns []string `json:"repo_patterns"`
		Password     *string  `json:"password"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	if body.Role != nil {
		if msg := patternsProblem(body.RepoPatterns); msg != "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
		}
		if err := h.store.UpdateUserAccess(username, *body.Role, body.RepoPatterns); err != nil {
			return userError(c, err)
		}
	}
	if body.Password != nil {
		if len(*body.Password) < 8 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*body.Password), bcrypt.DefaultCost)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, err500(err))
		}
		if err := h.store.UpdateUserPassword(username, string(hash)); err != nil {
			return userError(c, err)
		}
		// An admin resetting a password is locking someone out: end the
		// account's sessions (unless the admin is resetting their own).
		if target, err := h.store.GetUser(username); err == nil {
			if p, ok := auth.CurrentPrincipal(c); !ok || p.Username != username {
				_ = h.store.DeleteUserSessions(target.ID)
			}
		}
	}
	u, err := h.store.GetUser(username)
	if err != nil {
		return userError(c, err)
	}
	return c.JSON(http.StatusOK, u)
}

// Delete — DELETE /api/admin/users/:username. Self-deletion is refused, and
// the store refuses removing the last admin.
func (h *UsersHandler) Delete(c echo.Context) error {
	username := c.Param("username")
	if p, ok := auth.CurrentPrincipal(c); ok && p.Username == username {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "cannot delete your own account"})
	}
	if err := h.store.DeleteUser(username); err != nil {
		return userError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "user deleted"})
}

// usernameProblem returns why a username is unacceptable, or "".
func usernameProblem(username string) string {
	if username == "" {
		return "username is required"
	}
	if len(username) > 64 {
		return "username must be at most 64 characters"
	}
	for _, r := range username {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '.' || r == '_' || r == '-' || r == '@'
		if !ok {
			return "username may only contain letters, digits and . _ - @"
		}
	}
	// "anonymous" is the subject of anonymous-pull registry tokens.
	if strings.EqualFold(username, "anonymous") {
		return "\"anonymous\" is a reserved username"
	}
	return ""
}

// patternsProblem rejects repository patterns that can never match a valid
// repository name (and would silently lock the account out of everything).
func patternsProblem(patterns []string) string {
	for _, p := range patterns {
		probe := strings.ReplaceAll(p, "*", "x")
		if p == "" || !reference.ValidName(probe) {
			return "invalid repository pattern \"" + p + "\" — use repository names with * as wildcard, e.g. team-a/*"
		}
	}
	return ""
}

func userError(c echo.Context, err error) error {
	if errors.Is(err, store.ErrUserNotFound) {
		return c.JSON(http.StatusNotFound, err500(err))
	}
	return c.JSON(http.StatusBadRequest, err500(err))
}
