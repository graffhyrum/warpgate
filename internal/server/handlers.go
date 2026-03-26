package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/graffhyrum/warpgate/internal/provider"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	results := s.registry.CheckHealth(r.Context())
	status := http.StatusOK
	if !isFullyHealthy(results) {
		status = http.StatusServiceUnavailable
	}
	s.writeJSON(w, status, map[string]any{
		"healthy":   isFullyHealthy(results),
		"providers": toProviderViews(results),
	})
}

func (s *Server) handleListResources(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter, err := provider.ParseFilter(
		q.Get("provider"), q.Get("type"), q.Get("region"), q.Get("status"),
	)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	result := s.registry.ListAll(r.Context(), filter)
	warnings := result.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"resources": result.Resources,
		"count":     len(result.Resources),
		"warnings":  warnings,
	})
}

func (s *Server) handleGetResource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, r, http.StatusBadRequest, "missing resource id")
		return
	}

	res, err := s.registry.GetResource(r.Context(), id)
	if err != nil {
		if errors.Is(err, provider.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "resource not found")
			return
		}
		s.logger.Error("provider error", "request_id", RequestID(r.Context()), "error", err)
		s.writeError(w, r, http.StatusBadGateway, "upstream provider error")
		return
	}
	s.writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	results := s.registry.CheckHealth(r.Context())
	s.writeJSON(w, http.StatusOK, map[string]any{"providers": toProviderViews(results)})
}

// isFullyHealthy returns true if all providers report healthy.
// Returns false when no providers are registered (vacuous truth guard).
func isFullyHealthy(results []provider.HealthStatus) bool {
	if len(results) == 0 {
		return false
	}
	for _, hs := range results {
		if !hs.Healthy {
			return false
		}
	}
	return true
}

// providerView is the JSON representation of a provider's health.
type providerView struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	Latency string `json:"latency"`
	Error   string `json:"error,omitempty"`
}

func toProviderViews(results []provider.HealthStatus) []providerView {
	out := make([]providerView, len(results))
	for i, hs := range results {
		out[i] = providerView{
			Name:    hs.Provider,
			Healthy: hs.Healthy,
			Latency: hs.Latency.String(),
			Error:   hs.Error,
		}
	}
	return out
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.logger.Error("failed to encode response", "error", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"error":      message,
		"status":     status,
		"request_id": RequestID(r.Context()),
	}); err != nil {
		s.logger.Error("failed to encode error response", "error", err)
	}
}
