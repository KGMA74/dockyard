package server

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"maestro/config"
	"maestro/internal/auth"
	"maestro/internal/registry"
	"maestro/internal/storage"
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
	}

	switch m {
	case modeProxy:
		if cfg.RegistryURL == "" {
			log.Fatal("REGISTRY_MODE=proxy requires REGISTRY_URL to be set")
		}
		srv.proxy = registry.NewClient(cfg.RegistryURL, cfg.RegistryUsername, cfg.RegistryPassword)

	default:
		backend, err := storage.NewBackend(cfg)
		if err != nil {
			log.Fatalf("storage init failed: %v", err)
		}
		srv.backend = backend
		scheduleGC(backend)
	}

	if cfg.AuthUsername == "" || cfg.AuthPassword == "" || cfg.JWTSecret == "" {
		log.Fatal("AUTH_USERNAME, AUTH_PASSWORD and JWT_SECRET must be set")
	}
	authMgr, err := auth.New(cfg.AuthUsername, cfg.AuthPassword, cfg.JWTSecret, cfg.StoragePath)
	if err != nil {
		log.Fatalf("auth init failed: %v", err)
	}
	srv.auth = authMgr

	if cfg.V2AuthEnabled {
		hash, err := authMgr.PasswordHash()
		if err != nil {
			log.Fatalf("V2_AUTH_ENABLED=true but cannot read password hash: %v", err)
		}
		srv.v2AuthHash = hash
	}

	return &http.Server{
		Addr:         fmt.Sprintf(":%d", srv.port),
		Handler:      srv.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
}
