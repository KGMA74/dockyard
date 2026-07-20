package server

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"dockyard/internal/admin"
	"dockyard/internal/audit"
	"dockyard/internal/auth"
	"dockyard/internal/cosign"
	"dockyard/internal/export"
	"dockyard/internal/metrics"
	"dockyard/internal/retention"
	"dockyard/internal/scan"
	"dockyard/internal/store"
	"dockyard/internal/webhooks"
	uiassets "dockyard/internal/ui"
	"dockyard/internal/v2"
	"dockyard/internal/version"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"
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

	// Prometheus instrumentation — registered before the V2 interceptor so
	// registry traffic is measured too (paths are normalized to a bounded
	// label set in metrics.PathClass).
	if s.metricsEnabled {
		e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				start := time.Now()
				err := next(c)
				metrics.ObserveRequest(
					c.Request().Method,
					c.Request().URL.Path,
					c.Response().Status,
					time.Since(start),
				)
				return err
			}
		})
	}

	// CORS is off by default: the UI is embedded and served same-origin. Set
	// CORS_ALLOWED_ORIGINS to open the API to external browser clients.
	if len(s.corsAllowedOrigins) > 0 {
		e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOrigins:     s.corsAllowedOrigins,
			AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
			AllowHeaders:     []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
			AllowCredentials: true,
			MaxAge:           300,
		}))
	}

	// Loose per-IP rate limit on everything (docker pushes issue many requests
	// per layer — keep this generous). Strict limiter below guards the
	// credential endpoints against brute force.
	if s.rateLimitGlobalRPS > 0 {
		e.Use(middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
			Store: middleware.NewRateLimiterMemoryStoreWithConfig(middleware.RateLimiterMemoryStoreConfig{
				Rate:      rate.Limit(s.rateLimitGlobalRPS),
				Burst:     s.rateLimitGlobalRPS * 2,
				ExpiresIn: 3 * time.Minute,
			}),
		}))
	}
	strictLimiter := func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	if s.rateLimitLoginPerMin > 0 {
		strictLimiter = middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
			Store: middleware.NewRateLimiterMemoryStoreWithConfig(middleware.RateLimiterMemoryStoreConfig{
				Rate:      rate.Limit(float64(s.rateLimitLoginPerMin) / 60.0),
				Burst:     s.rateLimitLoginPerMin,
				ExpiresIn: 10 * time.Minute,
			}),
		})
	}

	// ── V2 engine — intercepted before the Echo router ────────────────────────
	var v2h http.Handler
	var mirror *v2.Mirror
	switch s.mode {
	case modeProxy:
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
	case modeMirror:
		mirror = v2.NewMirror(s.backend, s.events, s.proxy, s.mirrorTagTTL, s.signingPolicy)
		mirror.OnPull(store.NewPullTracker(s.store).Record)
		v2h = mirror
	default:
		h := v2.New(s.backend, s.events, s.signingPolicy)
		h.OnPull(store.NewPullTracker(s.store).Record)
		v2h = h
	}
	if s.metricsEnabled {
		if mirror != nil {
			metrics.SetMirrorSource(mirror.Stats)
		}
		if s.stats != nil {
			cache := s.stats
			metrics.SetStorageSource(func() (int64, int64, int64) {
				st, err := cache.Get()
				if err != nil {
					return 0, 0, 0
				}
				return int64(st.BlobCount), st.TotalSize, int64(st.RepoCount)
			})
		}
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
				// Credentials endpoint → strict brute-force limiter.
				return strictLimiter(v2Token)(c)
			}
			if v2.IsV2Path(path) {
				v2h.ServeHTTP(c.Response(), c.Request())
				return nil
			}
			return next(c)
		}
	})

	// ── Auth ──────────────────────────────────────────────────────────────────
	e.POST("/api/admin/auth/login", s.auth.Login, strictLimiter)
	e.POST("/api/admin/auth/logout", s.auth.Logout)
	// Refresh authenticates with the refresh token itself, not a JWT — it must
	// stay outside the middleware group (the access token may already be dead).
	e.POST("/api/admin/auth/refresh", s.auth.Refresh)

	// ── Admin API ─────────────────────────────────────────────────────────────
	api := e.Group("/api/admin", s.auth.Middleware(), auditor.AdminMiddleware())
	if s.mode == modeProxy {
		h := admin.NewRemote(s.proxy, s.signingPolicy, s.store)
		api.GET("/repositories", h.GetRepositories)
		api.GET("/repositories/tags", h.GetTags)
		api.GET("/repositories/manifest", h.GetManifestDetails)
		api.GET("/repositories/diff", h.GetTagDiff)
		api.GET("/repositories/search", h.Search)
		api.GET("/repositories/layer", h.GetLayerEntries)
		api.DELETE("/repositories/manifests", h.DeleteManifest, auth.RequireAdmin)
		api.DELETE("/repositories", h.DeleteRepository, auth.RequireAdmin)
		api.GET("/storage/stats", admin.NotSupported)
		api.GET("/storage/tree", admin.NotSupported)
		api.POST("/gc", admin.NotSupported)
	} else {
		h := admin.New(s.backend, s.signingPolicy, s.store)
		h.SetHub(s.events)
		api.GET("/repositories", h.GetRepositories)
		api.GET("/repositories/tags", h.GetTags)
		api.GET("/repositories/manifest", h.GetManifestDetails)
		api.GET("/repositories/diff", h.GetTagDiff)
		api.GET("/repositories/search", h.Search)
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

	// Storage insights — growth history + largest repositories.
	if s.backend != nil {
		ih := admin.NewInsights(s.backend, s.store)
		api.GET("/insights", ih.Get, auth.RequireAdmin)

		// Repository export/import (OCI image-layout tarballs).
		eh := export.NewHandler(s.backend)
		eh.SetHub(s.events)
		api.GET("/repositories/export", eh.Export, auth.RequireAdmin)
		api.POST("/repositories/import", eh.Import, auth.RequireAdmin)
	}

	// Retention policies — embedded/mirror only (needs local storage).
	if s.backend != nil {
		engine := retention.New(s.store, s.backend)
		engine.SetHub(s.events)
		rh := retention.NewHandler(engine, s.store)
		api.GET("/retention", rh.List, auth.RequireAdmin)
		api.POST("/retention", rh.Create, auth.RequireAdmin)
		api.DELETE("/retention/:id", rh.Delete, auth.RequireAdmin)
		api.POST("/retention/run", rh.Run, auth.RequireAdmin)
	}

	// Webhooks — outbox-backed deliveries for push/delete/retention/gc events.
	dispatcher := webhooks.NewDispatcher(s.store)
	dispatcher.Subscribe(s.events)
	wh := webhooks.NewHandler(s.store, dispatcher)
	api.GET("/webhooks", wh.List, auth.RequireAdmin)
	api.POST("/webhooks", wh.Create, auth.RequireAdmin)
	api.DELETE("/webhooks/:id", wh.Delete, auth.RequireAdmin)
	api.POST("/webhooks/:id/test", wh.Test, auth.RequireAdmin)

	// Vulnerability scanning — shells out to the `trivy` binary bundled in
	// Dockyard's own image. Standalone by default (TRIVY_SERVER_URL unset:
	// trivy manages its own vuln DB under the cache dir); set
	// TRIVY_SERVER_URL to defer to a shared/external trivy server instead.
	// Off unless SCAN_ENABLED is set.
	if s.scanEnabled {
		var resolver interface {
			GetManifest(name, reference string) ([]byte, string, error)
		}
		if s.mode == modeProxy {
			resolver = scan.RegistryResolver{Client: s.proxy}
		} else {
			resolver = s.backend
		}
		sd := scan.NewDispatcher(s.store, scan.Config{
			TrivyBin:       s.trivyBinPath,
			TrivyServerURL: s.trivyServerURL,
			TrivyCacheDir:  s.trivyCacheDir,
			RegistryURL:    fmt.Sprintf("localhost:%d", s.port),
			RegistryUser:   s.authUsername,
			RegistryPass:   s.authPassword,
			Insecure:       s.trivyInsecureRegistry,
			Timeout:        s.scanTimeout,
			MaxReportBytes: s.scanMaxReportBytes,
			DedupWindow:    s.scanDedupWindow,
		})
		sd.SetHub(s.events)
		sh := scan.NewHandler(s.store, sd, resolver, auditor)
		api.POST("/scans", sh.Trigger, auth.RequireAdmin)
		api.GET("/scans", sh.List, auth.RequireAdmin)
		api.GET("/scans/:id", sh.Get, auth.RequireAdmin)
		api.GET("/scans/:id/report", sh.Report, auth.RequireAdmin)
	}

	// Signed-push policy — status + per-repo overrides (admin only).
	cosignHandler := cosign.NewHandler(s.store, s.signingPolicy)
	api.GET("/signing", cosignHandler.Status, auth.RequireAdmin)
	api.GET("/signing/policies", cosignHandler.List, auth.RequireAdmin)
	api.POST("/signing/policies", cosignHandler.Create, auth.RequireAdmin)
	api.DELETE("/signing/policies/:id", cosignHandler.Delete, auth.RequireAdmin)

	// SSE feed of registry pushes. Registered outside the /api/admin group
	// because EventSource can't set an Authorization header — it authenticates
	// via a ?token= query param instead (see MiddlewareEventStream).
	e.GET("/api/admin/events", admin.Events(s.events), s.auth.MiddlewareEventStream())

	// ── Metrics ───────────────────────────────────────────────────────────────
	if s.metricsEnabled {
		e.GET("/metrics", echo.WrapHandler(metrics.Handler()))
	}

	// ── Health ────────────────────────────────────────────────────────────────
	e.GET("/health", func(c echo.Context) error {
		body := map[string]any{"status": "ok", "mode": string(s.mode), "version": version.Version}
		if s.proxy != nil { // proxy and mirror modes have an upstream
			if err := s.proxy.Ping(); err != nil {
				body["registry"] = "unreachable: " + err.Error()
			} else {
				body["registry"] = s.proxy.BaseURL()
			}
		}
		if mirror != nil {
			hits, misses := mirror.Stats()
			body["mirror"] = map[string]uint64{"hits": hits, "misses": misses}
		}
		if s.backend != nil {
			// Probe the storage backend and time it: on S3 this is a real
			// round trip, locally a stat() — either way it proves the backend
			// answers. The digest cannot exist, which is fine.
			start := time.Now()
			_, probeErr := s.backend.BlobExists("sha256:" + strings.Repeat("0", 64))
			st := map[string]any{
				"ok":         probeErr == nil,
				"latency_ms": time.Since(start).Milliseconds(),
			}
			if probeErr != nil {
				st["error"] = probeErr.Error()
				body["status"] = "degraded"
			}
			if s.stats != nil {
				if cached, err := s.stats.Get(); err == nil {
					st["blobs"] = cached.BlobCount
					st["bytes"] = cached.TotalSize
					st["repositories"] = cached.RepoCount
				}
			}
			if local, ok := s.backend.(interface{ Root() string }); ok {
				if free := diskFreeBytes(local.Root()); free > 0 {
					st["free_bytes"] = free
				}
			}
			body["storage"] = st
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
