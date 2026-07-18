package server

import (
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"dockyard/internal/admin"
	"dockyard/internal/audit"
	"dockyard/internal/auth"
	uiassets "dockyard/internal/ui"
	"dockyard/internal/v2"
	"dockyard/internal/version"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func (s *Server) RegisterRoutes() http.Handler {
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogMethod:  true,
		LogURI:     true,
		LogStatus:  true,
		LogLatency: true,
		LogError:   true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			attrs := []any{
				"method", v.Method,
				"uri", v.URI,
				"status", v.Status,
				"duration_ms", v.Latency / time.Millisecond,
			}
			if v.Error != nil {
				slog.Error("request", append(attrs, "err", v.Error)...)
			} else {
				slog.Info("request", attrs...)
			}
			return nil
		},
	}))
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     []string{"https://*", "http://*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// ── V2 engine — intercepted before the Echo router ────────────────────────
	var v2h http.Handler
	if s.mode == modeProxy {
		ph, err := v2.NewProxy(
			s.proxy.BaseURL(),
			s.proxy.Username(),
			s.proxy.Password(),
			s.events,
		)
		if err != nil {
			panic("invalid REGISTRY_URL: " + err.Error())
		}
		v2h = ph
	} else {
		v2h = v2.New(s.backend, s.events)
	}

	// Audit sits inside the auth wrapper so it sees the authenticated actor;
	// with auth disabled every V2 actor is recorded as "anonymous".
	auditor := audit.New(s.store)
	s.auth.SetAuditSink(auditor)
	v2h = auditor.V2Wrapper()(v2h)

	// Docker token auth: unauthenticated /v2/* requests get a Bearer challenge
	// pointing at /v2/token; Basic works as a fallback for plain docker login.
	if s.v2AuthEnabled {
		v2h = s.auth.V2Middleware(s.v2AnonymousPull)(v2h)
	}
	v2Token := s.auth.V2Token(s.v2AnonymousPull)

	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Request().URL.Path
			if path == "/v2/token" {
				// The token endpoint must stay outside the auth wrapper — it
				// is where clients go to authenticate in the first place.
				return v2Token(c)
			}
			if v2.IsV2Path(path) {
				v2h.ServeHTTP(c.Response(), c.Request())
				return nil
			}
			return next(c)
		}
	})

	// ── Auth ──────────────────────────────────────────────────────────────────
	e.POST("/api/admin/auth/login", s.auth.Login)
	e.POST("/api/admin/auth/logout", s.auth.Logout)
	// Refresh authenticates with the refresh token itself, not a JWT — it must
	// stay outside the middleware group (the access token may already be dead).
	e.POST("/api/admin/auth/refresh", s.auth.Refresh)

	// ── Admin API ─────────────────────────────────────────────────────────────
	api := e.Group("/api/admin", s.auth.Middleware(), auditor.AdminMiddleware())
	if s.mode == modeProxy {
		h := admin.NewRemote(s.proxy)
		api.GET("/repositories", h.GetRepositories)
		api.GET("/repositories/tags", h.GetTags)
		api.GET("/repositories/manifest", h.GetManifestDetails)
		api.GET("/repositories/layer", h.GetLayerEntries)
		api.DELETE("/repositories/manifests", h.DeleteManifest, auth.RequireAdmin)
		api.DELETE("/repositories", h.DeleteRepository, auth.RequireAdmin)
		api.GET("/storage/stats", admin.NotSupported)
		api.GET("/storage/tree", admin.NotSupported)
		api.POST("/gc", admin.NotSupported)
	} else {
		h := admin.New(s.backend)
		api.GET("/repositories", h.GetRepositories)
		api.GET("/repositories/tags", h.GetTags)
		api.GET("/repositories/manifest", h.GetManifestDetails)
		api.GET("/repositories/layer", h.GetLayerEntries)
		api.DELETE("/repositories/manifests", h.DeleteManifest, auth.RequireAdmin)
		api.DELETE("/repositories", h.DeleteRepository, auth.RequireAdmin)
		api.GET("/storage/stats", h.StorageStats)
		api.GET("/storage/tree", h.StorageTree)
		api.POST("/gc", h.GarbageCollect, auth.RequireAdmin)
	}
	api.POST("/auth/password", s.auth.ChangePassword)

	// User management — SQLite-backed, available in both modes, admin only.
	uh := admin.NewUsers(s.store)
	api.GET("/users", uh.List, auth.RequireAdmin)
	api.POST("/users", uh.Create, auth.RequireAdmin)
	api.PUT("/users/:username", uh.Update, auth.RequireAdmin)
	api.DELETE("/users/:username", uh.Delete, auth.RequireAdmin)

	// Session management — revoking kills the refresh token; outstanding
	// access tokens die within 15 minutes.
	api.GET("/sessions", s.auth.ListSessions, auth.RequireAdmin)
	api.DELETE("/sessions/:id", s.auth.RevokeSession, auth.RequireAdmin)

	// Audit trail of sensitive actions (logins, pushes, deletions, GC).
	api.GET("/audit", auditor.List, auth.RequireAdmin)

	// SSE feed of registry pushes. Registered outside the /api/admin group
	// because EventSource can't set an Authorization header — it authenticates
	// via a ?token= query param instead (see MiddlewareEventStream).
	e.GET("/api/admin/events", admin.Events(s.events), s.auth.MiddlewareEventStream())

	// ── Health ────────────────────────────────────────────────────────────────
	e.GET("/health", func(c echo.Context) error {
		body := map[string]string{"status": "ok", "mode": string(s.mode), "version": version.Version}
		if s.mode == modeProxy {
			if err := s.proxy.Ping(); err != nil {
				body["registry"] = "unreachable: " + err.Error()
			} else {
				body["registry"] = s.proxy.BaseURL()
			}
		}
		return c.JSON(http.StatusOK, body)
	})

	// ── UI SPA ────────────────────────────────────────────────────────────────
	staticFS, _ := fs.Sub(uiassets.Assets, "dist")
	fileServer := http.FileServer(http.FS(staticFS))

	serveIndex := func(c echo.Context) error {
		content, err := fs.ReadFile(staticFS, "index.html")
		if err != nil {
			return c.String(http.StatusServiceUnavailable, "UI not built — run: make ui\n")
		}
		c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
		return c.HTMLBlob(http.StatusOK, content)
	}

	// Root path — explicit route needed, /* does not match /
	e.GET("/", serveIndex)

	e.GET("/*", func(c echo.Context) error {
		path := c.Param("*")
		if f, err := staticFS.Open(path); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(c.Response(), c.Request())
			return nil
		}
		return serveIndex(c)
	})

	return e
}

// HelloWorldHandler kept for the existing test.
func (s *Server) HelloWorldHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"message": "Hello World"})
}
