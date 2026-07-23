package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dockyard/internal/server"
)

func gracefulShutdown(apiServer *http.Server, done chan bool) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()

	slog.Info("shutting down gracefully, press Ctrl+C again to force")
	stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := apiServer.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "err", err)
	}

	done <- true
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	srv, tracingShutdown := server.NewServer()
	defer func() {
		if err := tracingShutdown(context.Background()); err != nil {
			slog.Error("tracing shutdown failed", "err", err)
		}
	}()

	done := make(chan bool, 1)
	go gracefulShutdown(srv, done)

	// TLSConfig is set by the server when TLS_MODE is not off; cert and key
	// come from the config itself (static files, self-signed or ACME).
	var err error
	if srv.TLSConfig != nil {
		err = srv.ListenAndServeTLS("", "")
	} else {
		err = srv.ListenAndServe()
	}
	if err != nil && err != http.ErrServerClosed {
		slog.Error("http server error", "err", err)
		os.Exit(1)
	}

	<-done
	slog.Info("graceful shutdown complete")
}
