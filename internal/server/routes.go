package server

import (
	"log/slog"
	"net/http"
	"time"

	"maestro/internal/admin"
	"maestro/internal/auth"
	"maestro/internal/v2"

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
		)
		if err != nil {
			panic("invalid REGISTRY_URL: " + err.Error())
		}
		v2h = ph
	} else {
		v2h = v2.New(s.backend)
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
		api.DELETE("/repositories/manifests", h.DeleteManifest)
		api.GET("/storage/stats", admin.NotSupported)
		api.GET("/storage/tree", admin.NotSupported)
		api.POST("/gc", admin.NotSupported)
	} else {
		h := admin.New(s.backend)
		api.GET("/repositories", h.GetRepositories)
		api.GET("/repositories/tags", h.GetTags)
		api.DELETE("/repositories/manifests", h.DeleteManifest)
		api.GET("/storage/stats", h.StorageStats)
		api.GET("/storage/tree", h.StorageTree)
		api.POST("/gc", h.GarbageCollect)
	}
	api.POST("/auth/password", s.auth.ChangePassword)

	// ── Health ────────────────────────────────────────────────────────────────
	e.GET("/health", func(c echo.Context) error {
		body := map[string]string{"status": "ok", "mode": string(s.mode)}
		if s.mode == modeProxy {
			if err := s.proxy.Ping(); err != nil {
				body["registry"] = "unreachable: " + err.Error()
			} else {
				body["registry"] = s.proxy.BaseURL()
			}
		}
		return c.JSON(http.StatusOK, body)
	})

	return e
}

// HelloWorldHandler kept for the existing test.
func (s *Server) HelloWorldHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"message": "Hello World"})
}
