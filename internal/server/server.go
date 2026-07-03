package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"dockyard/config"
	"dockyard/internal/auth"
	"dockyard/internal/events"
	"dockyard/internal/registry"
	"dockyard/internal/storage"
)

type mode string

const (
	modeEmbedded mode = "embedded"
	modeProxy    mode = "proxy"
)

type Server struct {
	port          int
	mode          mode
	backend       storage.Backend
	proxy         *registry.Client
	auth          *auth.Manager
	events        *events.Hub
	v2AuthEnabled bool
	v2AuthHash    string
}

func NewServer() *http.Server {
	cfg := config.Load()
	printBanner(cfg)

	m := mode(cfg.RegistryMode)
	if m == "" {
		m = modeEmbedded
	}

	srv := &Server{
		port:          cfg.Port,
		mode:          m,
		v2AuthEnabled: cfg.V2AuthEnabled,
		events:        events.NewHub(),
	}

	switch m {
	case modeProxy:
		if cfg.RegistryURL == "" {
			slog.Error("REGISTRY_MODE=proxy requires REGISTRY_URL to be set")
			os.Exit(1)
		}
		srv.proxy = registry.NewClient(cfg.RegistryURL, cfg.RegistryUsername, cfg.RegistryPassword)

	default:
		backend, err := storage.NewBackend(cfg)
		if err != nil {
			slog.Error("storage init failed", "err", err)
			os.Exit(1)
		}
		srv.backend = backend
		scheduleGC(backend)
	}

	if cfg.AuthUsername == "" || cfg.AuthPassword == "" || cfg.JWTSecret == "" {
		slog.Error("AUTH_USERNAME, AUTH_PASSWORD and JWT_SECRET must be set")
		os.Exit(1)
	}
	authMgr, err := auth.New(cfg.AuthUsername, cfg.AuthPassword, cfg.JWTSecret, cfg.StoragePath)
	if err != nil {
		slog.Error("auth init failed", "err", err)
		os.Exit(1)
	}
	srv.auth = authMgr

	if cfg.V2AuthEnabled {
		hash, err := authMgr.PasswordHash()
		if err != nil {
			slog.Error("V2_AUTH_ENABLED=true but cannot read password hash", "err", err)
			os.Exit(1)
		}
		srv.v2AuthHash = hash
	}

	return &http.Server{
		Addr:              fmt.Sprintf(":%d", srv.port),
		Handler:           srv.RegisterRoutes(),
		IdleTimeout:       10 * time.Minute,
		ReadHeaderTimeout: 30 * time.Second,
	}
}
