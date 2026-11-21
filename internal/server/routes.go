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
	"dockyard/internal/quota"
	"dockyard/internal/replication"
	"dockyard/internal/retention"
	"dockyard/internal/scan"
	"dockyard/internal/store"
	uiassets "dockyard/internal/ui"
	"dockyard/internal/v2"
	"dockyard/internal/version"
	"dockyard/internal/webhooks"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"

	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
)

func (s *Server) RegisterRoutes() http.Handler {
	e := echo.New()
	e.HideBanner = true

	// Client IP resolution for rate limiting and the audit log — see
	// clientIPExtractor for why X-Forwarded-For is not believed blindly.
	ipExtractor, err := clientIPExtractor(s.trustedProxies)
	if err != nil {
		panic(err.Error())
	}
	e.IPExtractor = ipExtractor
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogMethod:  true,
		LogURI:     true,
		LogStatus:  true,
		LogLatency: true,
		LogError:   true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			attrs := []any{
				"method", v.Method,
				"uri", redactURI(v.URI),
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

	// OpenTelemetry tracing — off unless OTEL_EXPORTER_OTLP_ENDPOINT is set
	// (see internal/tracing), so this middleware is skipped entirely rather
	// than instrumenting into a no-op tracer.
	if s.tracingEnabled {
		e.Use(otelecho.Middleware("dockyard"))
	}

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
		mirror = v2.NewMirror(s.backend, s.events, s.proxy, s.mirrorTagTTL, s.signingPolicy, s.store)
		mirror.OnPull(store.NewPullTracker(s.store).Record)
		v2h = mirror
	default:
		h := v2.New(s.backend, s.events, s.signingPolicy, s.store)
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
	auditor.SetIPExtractor(ipExtractor)
	s.auth.SetAuditSink(auditor)

	// Vulnerability policy. Its state exists in every configuration (the
	// Security page reads it); scan-on-push and the pull gate need the
	// scanner. The gate sits inside the auth wrapper to know who pulls.
	policyState, err := scan.NewPolicyState(s.store)
	if err != nil {
		panic("vulnerability policy: " + err.Error())
	}
	var scanResolver interface {
		GetManifest(name, reference string) ([]byte, string, error)
	}
	if s.mode == modeProxy {
		scanResolver = scan.RegistryResolver{Client: s.proxy}
	} else {
		scanResolver = s.backend
	}
	var scanDispatcher *scan.Dispatcher
	if s.scanEnabled {
		scanDispatcher = scan.NewDispatcher(s.store, scan.Config{
			TrivyBin:       s.trivyBinPath,
			TrivyServerURL: s.trivyServerURL,
			TrivyCacheDir:  s.trivyCacheDir,
			RegistryURL:    fmt.Sprintf("localhost:%d", s.port),
			RegistryToken:  s.scannerTokenSource(),
			Insecure:       s.trivyInsecureRegistry,
			Offline:        s.trivyOffline,
			Timeout:        s.scanTimeout,
			MaxReportBytes: s.scanMaxReportBytes,
			DedupWindow:    s.scanDedupWindow,
		})
		scanDispatcher.SetHub(s.events)
		scan.ScanOnPush(s.events, policyState, scanResolver, scanDispatcher)
		go scan.BackfillVulnerabilities(s.store)
		v2h = scan.NewGate(policyState, scanResolver, auditor).Middleware(v2h)
	}

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

	// Registered after the V2 interceptor on purpose: it only wraps what is
	// left — the UI, the admin API and /health.
	e.Use(securityHeaders)

	// ── Auth ──────────────────────────────────────────────────────────────────
	e.POST("/api/admin/auth/login", s.auth.Login, strictLimiter)
	e.POST("/api/admin/auth/logout", s.auth.Logout)
	// Refresh authenticates with the refresh token itself, not a JWT — it must
	// stay outside the middleware group (the access token may already be dead).
	e.POST("/api/admin/auth/refresh", s.auth.Refresh)
	// Single sign-on: public by nature (that is how one signs in). The
	// exchange trades a one-time code for tokens: brute-force limited.
	e.GET("/api/admin/auth/oidc", s.auth.OIDCInfo)
	e.GET("/api/admin/auth/oidc/login", s.auth.OIDCLogin)
	e.GET("/api/admin/auth/oidc/callback", s.auth.OIDCCallback)
	e.POST("/api/admin/auth/oidc/exchange", s.auth.OIDCExchange, strictLimiter)

	// ── Admin API ─────────────────────────────────────────────────────────────
	api := e.Group("/api/admin", s.auth.Middleware(), auditor.AdminMiddleware())
	// Routes that address one repository (?name=) validate the name and apply
	// the caller's repository patterns — the same RBAC /v2 enforces. Listing
	// routes filter their results inside the handlers.
	repoScoped := auth.RequireRepoAccess("name")
	if s.mode == modeProxy {
		h := admin.NewRemote(s.proxy, s.signingPolicy, s.store)
		if s.scanEnabled {
			h.SetPullPolicy(policyState)
		}
		api.GET("/repositories", h.GetRepositories)
		api.GET("/repositories/tags", h.GetTags, repoScoped)
		api.GET("/repositories/manifest", h.GetManifestDetails, repoScoped)
		api.GET("/repositories/diff", h.GetTagDiff, repoScoped)
		api.GET("/repositories/search", h.Search)
		api.GET("/repositories/layer", h.GetLayerEntries, repoScoped)
		api.DELETE("/repositories/manifests", h.DeleteManifest, auth.RequireAdmin, repoScoped)
		api.DELETE("/repositories", h.DeleteRepository, auth.RequireAdmin, repoScoped)
		api.GET("/storage/stats", admin.NotSupported)
		api.GET("/storage/tree", admin.NotSupported, auth.RequireAdmin)
		api.POST("/gc", admin.NotSupported, auth.RequireAdmin)
	} else {
		h := admin.New(s.backend, s.signingPolicy, s.store)
		if s.scanEnabled {
			h.SetPullPolicy(policyState)
		}
		h.SetHub(s.events)
		h.SetGCGracePeriod(s.gcGracePeriod)
		api.GET("/repositories", h.GetRepositories)
		api.GET("/repositories/tags", h.GetTags, repoScoped)
		api.GET("/repositories/manifest", h.GetManifestDetails, repoScoped)
		api.GET("/repositories/diff", h.GetTagDiff, repoScoped)
		api.GET("/repositories/search", h.Search)
		api.GET("/repositories/layer", h.GetLayerEntries, repoScoped)
		api.DELETE("/repositories/manifests", h.DeleteManifest, auth.RequireAdmin, repoScoped)
		api.DELETE("/repositories", h.DeleteRepository, auth.RequireAdmin, repoScoped)
		api.GET("/storage/stats", h.StorageStats)
		// Lists every file of every repository: admin only.
		api.GET("/storage/tree", h.StorageTree, auth.RequireAdmin)
		api.POST("/gc", h.GarbageCollect, auth.RequireAdmin)
	}
	api.POST("/auth/password", s.auth.ChangePassword)
	api.GET("/auth/me", s.auth.Me)
	api.POST("/auth/cli-secret", s.auth.CreateCLISecret)
	api.DELETE("/auth/cli-secret", s.auth.DeleteCLISecret)

	// User management — SQLite-backed, available in both modes, admin only.
	uh := admin.NewUsers(s.store)
	api.GET("/users", uh.List, auth.RequireAdmin)
	api.POST("/users", uh.Create, auth.RequireAdmin)
	api.PUT("/users/:username", uh.Update, auth.RequireAdmin)
	api.DELETE("/users/:username", uh.Delete, auth.RequireAdmin)

	// Robot accounts — registry-only credentials for CI and clusters.
	rbh := admin.NewRobots(s.store)
	api.GET("/robots", rbh.List, auth.RequireAdmin)
	api.POST("/robots", rbh.Create, auth.RequireAdmin)
	api.POST("/robots/:id/secret", rbh.Rotate, auth.RequireAdmin)
	api.DELETE("/robots/:id", rbh.Delete, auth.RequireAdmin)

	// Session management — revoking kills the refresh token; outstanding
	// access tokens die within 15 minutes.
	api.GET("/sessions", s.auth.ListSessions, auth.RequireAdmin)
	api.DELETE("/sessions/:id", s.auth.RevokeSession, auth.RequireAdmin)
	// Everyone can see and end their own sessions (Settings page).
	api.GET("/sessions/mine", s.auth.ListMySessions)
	api.DELETE("/sessions/mine/:id", s.auth.RevokeMySession)

	// Effective server configuration, secrets left out.
	api.GET("/config", s.handleConfig, auth.RequireAdmin)
	// Getting-started checklist, ticked from the instance's actual state.
	api.GET("/setup", s.handleSetup, auth.RequireAdmin)
	// Single sign-on set up from the console (read-only when the deployment
	// configures it). The test runs discovery against an issuer URL.
	api.GET("/sso", s.auth.SSOSettings, auth.RequireAdmin)
	api.PUT("/sso", s.auth.UpdateSSOSettings, auth.RequireAdmin)
	api.POST("/sso/test", s.auth.TestSSOSettings, auth.RequireAdmin, strictLimiter)

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

	// Replication — push-based copy to other registries on tag push,
	// embedded/mirror only (needs local storage to read what was pushed).
	if s.backend != nil {
		replicator := replication.NewReplicator(s.store, s.backend)
		replicator.Subscribe(s.events)
		rh := replication.NewHandler(s.store, replicator)
		api.GET("/replication/targets", rh.List, auth.RequireAdmin)
		api.POST("/replication/targets", rh.Create, auth.RequireAdmin)
		api.DELETE("/replication/targets/:id", rh.Delete, auth.RequireAdmin)
		api.POST("/replication/targets/:id/test", rh.Test, auth.RequireAdmin)
	}

	// Vulnerability scanning — shells out to the `trivy` binary bundled in
	// Dockyard's own image. Standalone by default (TRIVY_SERVER_URL unset:
	// trivy manages its own vuln DB under the cache dir); set
	// TRIVY_SERVER_URL to defer to a shared/external trivy server instead.
	// Off unless SCAN_ENABLED is set.
	if scanDispatcher != nil {
		sh := scan.NewHandler(s.store, scanDispatcher, scanResolver, auditor)
		api.POST("/scans", sh.Trigger, auth.RequireAdmin)
		api.GET("/scans", sh.List, auth.RequireAdmin)
		api.GET("/scans/:id", sh.Get, auth.RequireAdmin)
		api.GET("/scans/:id/report", sh.Report, auth.RequireAdmin)
	}

	// Vulnerability policy — scan on push, pull blocking, CVE allowlist.
	ph := scan.NewPolicyHandler(s.store, policyState, s.scanEnabled)
	api.GET("/vulnerability-policy", ph.Get, auth.RequireAdmin)
	api.PUT("/vulnerability-policy", ph.Update, auth.RequireAdmin)
	api.POST("/vulnerability-policy/allowlist", ph.Allow, auth.RequireAdmin)
	api.DELETE("/vulnerability-policy/allowlist/:cve", ph.Disallow, auth.RequireAdmin)

	// Signed-push policy — status + per-repo overrides (admin only).
	cosignHandler := cosign.NewHandler(s.store, s.signingPolicy)
	api.GET("/signing", cosignHandler.Status, auth.RequireAdmin)
	api.GET("/signing/policies", cosignHandler.List, auth.RequireAdmin)
	api.POST("/signing/policies", cosignHandler.Create, auth.RequireAdmin)
	api.DELETE("/signing/policies/:id", cosignHandler.Delete, auth.RequireAdmin)

	// Byte quotas per repo/user + usage (admin only). Enforcement happens in
	// internal/v2 (embedded/mirror modes only); this is just the policy CRUD.
	quotaHandler := quota.NewHandler(s.store)
	api.GET("/quotas", quotaHandler.List, auth.RequireAdmin)
	api.PUT("/quotas", quotaHandler.Set, auth.RequireAdmin)
	api.DELETE("/quotas/:id", quotaHandler.Delete, auth.RequireAdmin)
	api.POST("/quotas/usage/reset", quotaHandler.ResetUsage, auth.RequireAdmin)

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
		// index.html names the hashed bundles: it must be revalidated on
		// every load or a deploy keeps serving the previous UI.
		c.Response().Header().Set("Cache-Control", "no-cache")
		return c.HTMLBlob(http.StatusOK, content)
	}

	// Root path — explicit route needed, /* does not match /
	e.GET("/", serveIndex)

	e.GET("/*", func(c echo.Context) error {
		path := c.Param("*")
		// An unknown API path is a client error, not a page: answering it
		// with the SPA shell (200 + HTML) hid typos and broke JSON clients.
		if path == "api" || strings.HasPrefix(path, "api/") {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "unknown API endpoint"})
		}
		if f, err := staticFS.Open(path); err == nil {
			info, statErr := f.Stat()
			_ = f.Close()
			if statErr == nil && !info.IsDir() {
				if strings.HasPrefix(path, "assets/") {
					// Vite content-hashes everything under assets/.
					c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					c.Response().Header().Set("Cache-Control", "no-cache")
				}
				fileServer.ServeHTTP(c.Response(), c.Request())
				return nil
			}
		}
		// Anything else is a client-side route (/images/team/app, /storage…).
		return serveIndex(c)
	})

	return e
}
