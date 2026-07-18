package server

import (
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"dockyard/internal/admin"
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

	// Wrap /v2/* with basic auth when enabled
	if s.v2AuthEnabled && s.v2AuthHash != "" {
		v2h = auth.BasicAuthMiddleware(s.auth.Username(), s.v2AuthHash)(v2h)
	}

	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if v2.IsV2Path(c.Request().URL.Path) {
				v2h.ServeHTTP(c.Response(), c.Request())
				return nil
			}
			return next(c)
		}
	})

	// ── Auth ──────────────────────────────────────────────────────────────────
	e.POST("/api/admin/auth/login", s.auth.Login)
	e.POST("/api/admin/auth/logout", s.auth.Logout)

	// ── Admin API ─────────────────────────────────────────────────────────────
	api := e.Group("/api/admin", s.auth.Middleware())
	if s.mode == modeProxy {
		h := admin.NewRemote(s.proxy)
		api.GET("/repositories", h.GetRepositories)
		api.GET("/repositories/tags", h.GetTags)
		api.GET("/repositories/manifest", h.GetManifestDetails)
		api.GET("/repositories/layer", h.GetLayerEntries)
		api.DELETE("/repositories/manifests", h.DeleteManifest)
		api.DELETE("/repositories", h.DeleteRepository)
		api.GET("/storage/stats", admin.NotSupported)
		api.GET("/storage/tree", admin.NotSupported)
		api.POST("/gc", admin.NotSupported)
	} else {
		h := admin.New(s.backend)
		api.GET("/repositories", h.GetRepositories)
		api.GET("/repositories/tags", h.GetTags)
		api.GET("/repositories/manifest", h.GetManifestDetails)
		api.GET("/repositories/layer", h.GetLayerEntries)
		api.DELETE("/repositories/manifests", h.DeleteManifest)
		api.DELETE("/repositories", h.DeleteRepository)
		api.GET("/storage/stats", h.StorageStats)
		api.GET("/storage/tree", h.StorageTree)
		api.POST("/gc", h.GarbageCollect)
	}
	api.POST("/auth/password", s.auth.ChangePassword)

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
