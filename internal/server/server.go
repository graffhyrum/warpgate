// Package server implements the HTTP server, route registration,
// middleware stack, and graceful shutdown for warpgate.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/graffhyrum/warpgate/internal/provider"
)

// Registry defines the interface the server needs from the provider registry.
type Registry interface {
	ListAll(ctx context.Context, filter provider.ResourceFilter) provider.ListResult
	GetResource(ctx context.Context, id string) (*provider.Resource, error)
	CheckHealth(ctx context.Context) []provider.HealthStatus
	Providers() []string
}

// Server is the warpgate HTTP server.
type Server struct {
	httpServer *http.Server
	registry   Registry
	logger     *slog.Logger
}

// New creates a configured Server.
func New(port int, registry Registry, logger *slog.Logger) *Server {
	s := &Server{
		registry: registry,
		logger:   logger,
	}

	mux := http.NewServeMux()

	// Routes (Go 1.22+ pattern syntax)
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)
	mux.HandleFunc("GET /api/v1/resources", s.handleListResources)
	mux.HandleFunc("GET /api/v1/resources/{id}", s.handleGetResource)
	mux.HandleFunc("GET /api/v1/providers", s.handleListProviders)

	// Middleware stack (outermost first): recovery -> request ID -> security -> logging -> routes
	var handler http.Handler = mux
	handler = loggingMiddleware(logger, handler)
	handler = securityHeadersMiddleware(handler)
	handler = requestIDMiddleware(handler)
	handler = recoveryMiddleware(logger, handler)

	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	return s
}

// Start begins listening. Blocks until the server exits.
func (s *Server) Start() error {
	s.logger.Info("server starting", "addr", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Handler returns the server's HTTP handler (for testing).
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("server shutting down")
	return s.httpServer.Shutdown(ctx)
}
