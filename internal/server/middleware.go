package server

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

type contextKey string

const requestIDKey contextKey = "request_id"

// RequestID returns the request ID from the context, or empty string.
func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// requestIDMiddleware generates a UUID request ID, stores it in context,
// and sets the X-Request-Id response header.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := uuid.New().String()
		ctx := context.WithValue(r.Context(), requestIDKey, rid)
		w.Header().Set("X-Request-Id", rid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// securityHeadersMiddleware sets baseline security response headers.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs each request with method, path, status, duration, and request ID.
func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(sw, r)

		logger.Info("request",
			"request_id", RequestID(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// recoveryMiddleware catches panics and returns a 500 error.
// If headers have already been sent, it closes the connection instead of
// attempting to write a new status code (which would be silently ignored).
func recoveryMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("panic recovered",
					"request_id", RequestID(r.Context()),
					"panic", rec,
					"stack", string(debug.Stack()),
				)
				// Only write error response if headers haven't been sent yet
				if sw, ok := w.(*statusWriter); ok && !sw.Written() {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":"internal server error","status":500}`))
				}
				// If already written, the connection will be closed by the deferred panic
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// statusWriter wraps ResponseWriter to capture the status code and track writes.
type statusWriter struct {
	http.ResponseWriter
	status  int
	written atomic.Bool
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.written.Store(true)
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Write(b []byte) (int, error) {
	sw.written.Store(true)
	return sw.ResponseWriter.Write(b)
}

// Written returns true if WriteHeader or Write has been called.
func (sw *statusWriter) Written() bool {
	return sw.written.Load()
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController compatibility.
func (sw *statusWriter) Unwrap() http.ResponseWriter {
	return sw.ResponseWriter
}
