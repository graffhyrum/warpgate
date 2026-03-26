// Package main is the entry point for the warpgate server.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/graffhyrum/warpgate/internal/config"
	"github.com/graffhyrum/warpgate/internal/mock"
	"github.com/graffhyrum/warpgate/internal/provider"
	"github.com/graffhyrum/warpgate/internal/server"
)

func main() {
	cfg := config.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(logger)

	registry := provider.NewRegistry(cfg.ProviderTimeout)
	for _, p := range mock.DefaultProviders() {
		registry.Register(p)
		logger.Info("registered provider", "name", p.Name())
	}

	srv := server.New(cfg.Port, registry, logger)

	// Graceful shutdown on SIGINT/SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.Start()
	}()

	logger.Info("server ready", "port", cfg.Port)

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
		os.Exit(1)
	}

	// Drain the server goroutine
	if err := <-serverErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server error", "error", err)
	}
	logger.Info("server stopped")
}
