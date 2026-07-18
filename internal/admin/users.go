package admin

import (
	"errors"
	"net/http"

	"dockyard/internal/auth"
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
	if body.Username == "" || !store.ValidRole(body.Role) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "username and a valid role (admin, pusher, reader) are required"})
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

func userError(c echo.Context, err error) error {
	if errors.Is(err, store.ErrUserNotFound) {
		return c.JSON(http.StatusNotFound, err500(err))
	}
	return c.JSON(http.StatusBadRequest, err500(err))
}
