package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"dockyard/config"
	"dockyard/internal/auth"
	"dockyard/internal/cosign"
	"dockyard/internal/events"
	"dockyard/internal/registry"
	"dockyard/internal/retention"
	"dockyard/internal/storage"
	"dockyard/internal/store"
	"dockyard/internal/tlsutil"
	"dockyard/internal/tracing"
)

type mode string

const (
	modeEmbedded mode = "embedded"
	modeProxy    mode = "proxy"
	modeMirror   mode = "mirror"
)

type Server struct {
	port            int
	mode            mode
	backend         storage.Backend
	proxy           *registry.Client
	auth            *auth.Manager
	store           *store.Store
	events          *events.Hub
	v2AuthEnabled   bool
	v2AnonymousPull bool
	authUsername    string
	authPassword    string

	corsAllowedOrigins   []string
	rateLimitLoginPerMin int
	rateLimitGlobalRPS   int
	trustedProxies       []string
	gcGracePeriod        time.Duration
	mirrorTagTTL         time.Duration
	metricsEnabled       bool
	stats                *statsCache

	scanEnabled           bool
	trivyServerURL        string
	trivyBinPath          string
	trivyCacheDir         string
	trivyDBSeedDir        string
	trivyOffline          bool
	scanTimeout           time.Duration
	scanMaxReportBytes    int64
	scanDedupWindow       time.Duration
	trivyInsecureRegistry bool

	signingPolicy *cosign.Policy

	tracingEnabled bool

	// configSummary is what GET /api/admin/config shows: built once from
	// the loaded config, secrets left out (see newConfigSummary).
	configSummary []ConfigGroup
}

// NewServer builds the HTTP server. The second return value flushes and
// closes the OpenTelemetry exporter (a no-op if tracing was never enabled) —
// callers should invoke it during graceful shutdown.
func NewServer() (*http.Server, func(context.Context) error) {
	cfg := config.Load()
	printBanner(cfg)

	tracingShutdown, tracingEnabled := tracing.Init(context.Background())

	m := mode(cfg.RegistryMode)
	if m == "" {
		m = modeEmbedded
	}

	srv := &Server{
		port:            cfg.Port,
		mode:            m,
		v2AuthEnabled:   cfg.V2AuthEnabled,
		v2AnonymousPull: cfg.V2AnonymousPull,
		authUsername:    cfg.AuthUsername,
		authPassword:    cfg.AuthPassword,
		events:          events.NewHub(),

		corsAllowedOrigins:   cfg.CORSAllowedOrigins,
		rateLimitLoginPerMin: cfg.RateLimitLoginPerMin,
		rateLimitGlobalRPS:   cfg.RateLimitGlobalRPS,
		trustedProxies:       cfg.TrustedProxies,
		gcGracePeriod:        cfg.GCGracePeriod,
		metricsEnabled:       cfg.MetricsEnabled,

		scanEnabled:           cfg.ScanEnabled,
		trivyServerURL:        cfg.TrivyServerURL,
		trivyBinPath:          cfg.TrivyBinPath,
		trivyCacheDir:         cfg.TrivyCacheDir,
		trivyDBSeedDir:        cfg.TrivyDBSeedDir,
		trivyOffline:          cfg.TrivyOffline,
		scanTimeout:           cfg.ScanTimeout,
		scanMaxReportBytes:    cfg.ScanMaxReportBytes,
		scanDedupWindow:       cfg.ScanDedupWindow,
		trivyInsecureRegistry: cfg.TrivyInsecureRegistry,

		tracingEnabled: tracingEnabled,
	}
	if srv.trivyCacheDir == "" {
		srv.trivyCacheDir = filepath.Join(cfg.StoragePath, "trivy-cache")
	}
	if srv.scanEnabled {
		if err := os.MkdirAll(srv.trivyCacheDir, 0700); err != nil {
			slog.Error("scan: failed to create trivy cache dir", "dir", srv.trivyCacheDir, "err", err)
			os.Exit(1)
		}
		seedTrivyCache(srv.trivyCacheDir, srv.trivyDBSeedDir)
	}

	switch m {
	case modeProxy:
		if cfg.RegistryURL == "" {
			slog.Error("REGISTRY_MODE=proxy requires REGISTRY_URL to be set")
			os.Exit(1)
		}
		srv.proxy = registry.NewClient(cfg.RegistryURL, cfg.RegistryUsername, cfg.RegistryPassword)

	case modeMirror:
		// Pull-through cache: local storage like embedded + an upstream
		// client for cache misses.
		if cfg.RegistryURL == "" {
			slog.Error("REGISTRY_MODE=mirror requires REGISTRY_URL (the upstream registry)")
			os.Exit(1)
		}
		srv.proxy = registry.NewClient(cfg.RegistryURL, cfg.RegistryUsername, cfg.RegistryPassword)
		srv.mirrorTagTTL = cfg.MirrorTagTTL
		backend, err := storage.NewBackend(cfg)
		if err != nil {
			slog.Error("storage init failed", "err", err)
			os.Exit(1)
		}
		srv.backend = backend

	default:
		backend, err := storage.NewBackend(cfg)
		if err != nil {
			slog.Error("storage init failed", "err", err)
			os.Exit(1)
		}
		srv.backend = backend
	}

	if srv.backend != nil {
		srv.stats = newStatsCache(srv.backend, 30*time.Second)
	}

	// The SQLite store lives alongside registry data in both modes: proxy mode
	// also needs users/sessions/audit even though blobs stay upstream.
	st, err := store.Open(filepath.Join(cfg.StoragePath, "dockyard.db"))
	if err != nil {
		slog.Error("store init failed", "err", err)
		os.Exit(1)
	}
	srv.store = st

	cosignKeys, err := cosign.LoadPublicKeys(cfg.CosignPublicKeysDir)
	if err != nil {
		slog.Error("cosign: failed to load public keys", "err", err)
		os.Exit(1)
	}
	srv.signingPolicy = cosign.NewPolicy(cfg.RequireSignedPush, cosignKeys, st)
	srv.configSummary = newConfigSummary(cfg, m, len(cosignKeys), tracingEnabled, cfg.GCGracePeriod.String())
	if cfg.RequireSignedPush && len(cosignKeys) == 0 {
		slog.Warn("REQUIRE_SIGNED_PUSH is on but no cosign public keys are configured — all gated pushes will be rejected until COSIGN_PUBLIC_KEYS_DIR is set")
	}

	// Daily maintenance (retention then GC) needs both the backend and the
	// policy store, hence scheduled after both exist.
	if srv.backend != nil {
		scheduleMaintenance(srv.backend, retention.New(st, srv.backend), srv.events, srv.gcGracePeriod)
		go srv.sampleStatsLoop()
	}

	if _, err := clientIPExtractor(cfg.TrustedProxies); err != nil {
		slog.Error("invalid configuration", "err", err)
		os.Exit(1)
	}
	if cfg.AuthUsername == "" || cfg.AuthPassword == "" || cfg.JWTSecret == "" {
		slog.Error("AUTH_USERNAME, AUTH_PASSWORD and JWT_SECRET must be set")
		os.Exit(1)
	}
	authMgr, err := auth.New(cfg.AuthUsername, cfg.AuthPassword, cfg.JWTSecret, cfg.JWTSecretPrevious, cfg.StoragePath, st)
	if err != nil {
		slog.Error("auth init failed", "err", err)
		os.Exit(1)
	}
	srv.auth = authMgr
	if cfg.OIDCIssuerURL != "" {
		if cfg.OIDCClientID == "" {
			slog.Error("OIDC_ISSUER_URL is set but OIDC_CLIENT_ID is not")
			os.Exit(1)
		}
		authMgr.EnableOIDC(auth.OIDCConfig{
			IssuerURL:     cfg.OIDCIssuerURL,
			ClientID:      cfg.OIDCClientID,
			ClientSecret:  cfg.OIDCClientSecret,
			RedirectURL:   cfg.OIDCRedirectURL,
			Scopes:        cfg.OIDCScopes,
			UsernameClaim: cfg.OIDCUsernameClaim,
			GroupsClaim:   cfg.OIDCGroupsClaim,
			AdminGroups:   cfg.OIDCAdminGroups,
			PusherGroups:  cfg.OIDCPusherGroups,
			ReaderGroups:  cfg.OIDCReaderGroups,
			DefaultRole:   cfg.OIDCDefaultRole,
			DisplayName:   cfg.OIDCDisplayName,
		})
		slog.Info("single sign-on enabled by the deployment", "issuer", cfg.OIDCIssuerURL)
	} else if cfg.OIDCClientSecret != "" {
		// Configured from the console, with the secret kept in the deployment.
		authMgr.SetOIDCEnvClientSecret(cfg.OIDCClientSecret)
	}

	tlsConfig, err := tlsutil.Config(tlsutil.Options{
		Mode:      cfg.TLSMode,
		CertFile:  cfg.TLSCertFile,
		KeyFile:   cfg.TLSKeyFile,
		Domain:    cfg.TLSDomain,
		ACMEEmail: cfg.TLSACMEEmail,
		Dir:       filepath.Join(cfg.StoragePath, "tls"),
	})
	if err != nil {
		slog.Error("tls init failed", "err", err)
		os.Exit(1)
	}
	if tlsConfig != nil {
		slog.Info("tls enabled", "mode", cfg.TLSMode)
	}

	return &http.Server{
		Addr:              fmt.Sprintf(":%d", srv.port),
		Handler:           srv.RegisterRoutes(),
		IdleTimeout:       10 * time.Minute,
		ReadHeaderTimeout: 30 * time.Second,
		TLSConfig:         tlsConfig,
	}, tracingShutdown
}
