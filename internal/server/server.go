package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"dockyard/config"
	"dockyard/internal/auth"
	"dockyard/internal/events"
	"dockyard/internal/registry"
	"dockyard/internal/storage"
	"dockyard/internal/store"
	"dockyard/internal/tlsutil"
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

	corsAllowedOrigins   []string
	rateLimitLoginPerMin int
	rateLimitGlobalRPS   int
	mirrorTagTTL         time.Duration
	metricsEnabled       bool
}

func NewServer() *http.Server {
	cfg := config.Load()
	printBanner(cfg)

	m := mode(cfg.RegistryMode)
	if m == "" {
		m = modeEmbedded
	}

	srv := &Server{
		port:            cfg.Port,
		mode:            m,
		v2AuthEnabled:   cfg.V2AuthEnabled,
		v2AnonymousPull: cfg.V2AnonymousPull,
		events:          events.NewHub(),

		corsAllowedOrigins:   cfg.CORSAllowedOrigins,
		rateLimitLoginPerMin: cfg.RateLimitLoginPerMin,
		rateLimitGlobalRPS:   cfg.RateLimitGlobalRPS,
		metricsEnabled:       cfg.MetricsEnabled,
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
		scheduleGC(backend)

	default:
		backend, err := storage.NewBackend(cfg)
		if err != nil {
			slog.Error("storage init failed", "err", err)
			os.Exit(1)
		}
		srv.backend = backend
		scheduleGC(backend)
	}

	// The SQLite store lives alongside registry data in both modes: proxy mode
	// also needs users/sessions/audit even though blobs stay upstream.
	st, err := store.Open(filepath.Join(cfg.StoragePath, "dockyard.db"))
	if err != nil {
		slog.Error("store init failed", "err", err)
		os.Exit(1)
	}
	srv.store = st

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
	}
}
