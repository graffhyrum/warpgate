package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// Registry holds registered providers and orchestrates fan-out queries.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	timeout   time.Duration
}

// NewRegistry creates a registry with the given per-provider timeout.
func NewRegistry(timeout time.Duration) *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		timeout:   timeout,
	}
}

// Register adds a provider. Overwrites if name already exists.
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

// Provider returns a single provider by name, or nil if not found.
func (r *Registry) Provider(name string) Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers[name]
}

// Providers returns the names of all registered providers.
func (r *Registry) Providers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// snapshotProviders returns a copy of the providers map under the lock,
// then releases the lock so callers can perform I/O without blocking Register.
func (r *Registry) snapshotProviders() map[string]Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snap := make(map[string]Provider, len(r.providers))
	for name, p := range r.providers {
		snap[name] = p
	}
	return snap
}

// ListResult holds the aggregated result of a fan-out ListResources call.
type ListResult struct {
	Resources []Resource
	Warnings  []string
}

// ListAll queries providers concurrently with per-provider timeouts.
// If filter.Provider is set, only that provider is queried.
func (r *Registry) ListAll(ctx context.Context, filter ResourceFilter) ListResult {
	snap := r.snapshotProviders()

	targets := make(map[string]Provider)
	if filter.Provider != "" {
		if p, ok := snap[filter.Provider]; ok {
			targets[filter.Provider] = p
		} else {
			return ListResult{Warnings: []string{fmt.Sprintf("provider %q not found", filter.Provider)}}
		}
	} else {
		targets = snap
	}

	var (
		mu        sync.Mutex
		resources []Resource
		warnings  []string
	)

	var g errgroup.Group

	for name, p := range targets {
		g.Go(func() error {
			pCtx, cancel := context.WithTimeout(ctx, r.timeout)
			defer cancel()

			res, err := p.ListResources(pCtx, filter)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("provider %s: %s", name, err))
				return nil // partial failure — don't abort other providers
			}
			resources = append(resources, res...)
			return nil
		})
	}

	_ = g.Wait() // errors collected as warnings
	if resources == nil {
		resources = []Resource{}
	}
	return ListResult{Resources: resources, Warnings: warnings}
}

// GetResource searches all providers concurrently for a resource by ID.
// Returns ErrNotFound only when all providers report not-found.
// Returns a wrapped provider error if any provider fails with a non-not-found error.
func (r *Registry) GetResource(ctx context.Context, id string) (*Resource, error) {
	snap := r.snapshotProviders()

	var (
		mu      sync.Mutex
		found   *Resource
		realErr error
	)

	var g errgroup.Group
	for name, p := range snap {
		g.Go(func() error {
			pCtx, cancel := context.WithTimeout(ctx, r.timeout)
			defer cancel()

			res, err := p.GetResource(pCtx, id)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				found = res
				return nil
			}
			if !errors.Is(err, ErrNotFound) {
				realErr = fmt.Errorf("provider %s: %w", name, err)
			}
			return nil
		})
	}

	_ = g.Wait()
	if found != nil {
		return found, nil
	}
	if realErr != nil {
		return nil, realErr
	}
	return nil, ErrNotFound
}

// CheckHealth returns health status for all providers.
func (r *Registry) CheckHealth(ctx context.Context) []HealthStatus {
	snap := r.snapshotProviders()

	var (
		mu      sync.Mutex
		results []HealthStatus
	)

	var g errgroup.Group
	for _, p := range snap {
		g.Go(func() error {
			pCtx, cancel := context.WithTimeout(ctx, r.timeout)
			defer cancel()
			hs := p.CheckHealth(pCtx)
			mu.Lock()
			results = append(results, hs)
			mu.Unlock()
			return nil
		})
	}

	_ = g.Wait()
	return results
}
