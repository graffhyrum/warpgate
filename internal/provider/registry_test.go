package provider_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/graffhyrum/warpgate/internal/provider"
)

// stubProvider implements provider.Provider for testing.
type stubProvider struct {
	name      string
	resources []provider.Resource
	err       error
	healthy   bool
	latency   time.Duration
}

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) ListResources(_ context.Context, _ provider.ResourceFilter) ([]provider.Resource, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.resources, nil
}

func (s *stubProvider) GetResource(_ context.Context, id string) (*provider.Resource, error) {
	if s.err != nil {
		return nil, s.err
	}
	for _, r := range s.resources {
		if r.ID == id {
			res := r
			return &res, nil
		}
	}
	return nil, provider.ErrNotFound
}

func (s *stubProvider) CheckHealth(_ context.Context) provider.HealthStatus {
	return provider.HealthStatus{Provider: s.name, Healthy: s.healthy, Latency: s.latency}
}

func TestRegistry_ListAll_FanOut(t *testing.T) {
	t.Parallel()
	reg := provider.NewRegistry(5 * time.Second)

	p1 := &stubProvider{
		name:    "alpha",
		healthy: true,
		resources: []provider.Resource{
			{ID: "a1", Provider: "alpha", Type: provider.TypeCompute},
		},
	}
	p2 := &stubProvider{
		name:    "beta",
		healthy: true,
		resources: []provider.Resource{
			{ID: "b1", Provider: "beta", Type: provider.TypeStorage},
		},
	}
	reg.Register(p1)
	reg.Register(p2)

	result := reg.ListAll(context.Background(), provider.ResourceFilter{})

	if len(result.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(result.Resources))
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", result.Warnings)
	}
}

func TestRegistry_ListAll_PartialFailure(t *testing.T) {
	t.Parallel()
	reg := provider.NewRegistry(5 * time.Second)

	good := &stubProvider{
		name:    "good",
		healthy: true,
		resources: []provider.Resource{
			{ID: "g1", Provider: "good"},
		},
	}
	bad := &stubProvider{
		name: "bad",
		err:  errors.New("connection refused"),
	}
	reg.Register(good)
	reg.Register(bad)

	result := reg.ListAll(context.Background(), provider.ResourceFilter{})

	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 resource from good provider, got %d", len(result.Resources))
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(result.Warnings))
	}
}

func TestRegistry_ListAll_SingleProvider(t *testing.T) {
	t.Parallel()
	reg := provider.NewRegistry(5 * time.Second)
	reg.Register(&stubProvider{
		name:      "alpha",
		resources: []provider.Resource{{ID: "a1", Provider: "alpha"}},
	})
	reg.Register(&stubProvider{
		name:      "beta",
		resources: []provider.Resource{{ID: "b1", Provider: "beta"}},
	})

	result := reg.ListAll(context.Background(), provider.ResourceFilter{Provider: "alpha"})

	if len(result.Resources) != 1 || result.Resources[0].ID != "a1" {
		t.Fatalf("expected only alpha's resource, got %v", result.Resources)
	}
}

func TestRegistry_ListAll_UnknownProvider(t *testing.T) {
	t.Parallel()
	reg := provider.NewRegistry(5 * time.Second)

	result := reg.ListAll(context.Background(), provider.ResourceFilter{Provider: "ghost"})

	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning for unknown provider, got %d", len(result.Warnings))
	}
}

func TestRegistry_GetResource_Found(t *testing.T) {
	t.Parallel()
	reg := provider.NewRegistry(5 * time.Second)
	reg.Register(&stubProvider{
		name:      "alpha",
		resources: []provider.Resource{{ID: "a1", Name: "found-me", Provider: "alpha"}},
	})

	res, err := reg.GetResource(context.Background(), "a1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Name != "found-me" {
		t.Fatalf("expected name 'found-me', got %q", res.Name)
	}
}

func TestRegistry_GetResource_NotFound(t *testing.T) {
	t.Parallel()
	reg := provider.NewRegistry(5 * time.Second)
	reg.Register(&stubProvider{name: "alpha"})

	_, err := reg.GetResource(context.Background(), "nonexistent")
	if !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRegistry_GetResource_DistinguishesRealErrors(t *testing.T) {
	t.Parallel()
	reg := provider.NewRegistry(5 * time.Second)
	reg.Register(&stubProvider{
		name: "broken",
		err:  errors.New("connection timeout"),
	})

	_, err := reg.GetResource(context.Background(), "any-id")
	if errors.Is(err, provider.ErrNotFound) {
		t.Fatal("expected a real error, not ErrNotFound")
	}
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestRegistry_CheckHealth(t *testing.T) {
	t.Parallel()
	reg := provider.NewRegistry(5 * time.Second)
	reg.Register(&stubProvider{name: "alpha", healthy: true, latency: 10 * time.Millisecond})
	reg.Register(&stubProvider{name: "beta", healthy: false, latency: 100 * time.Millisecond})

	results := reg.CheckHealth(context.Background())
	if len(results) != 2 {
		t.Fatalf("expected 2 health results, got %d", len(results))
	}

	healthMap := make(map[string]bool)
	for _, hs := range results {
		healthMap[hs.Provider] = hs.Healthy
	}
	if !healthMap["alpha"] {
		t.Error("alpha should be healthy")
	}
	if healthMap["beta"] {
		t.Error("beta should be unhealthy")
	}
}

func TestParseFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		typ     string
		status  string
		wantErr bool
	}{
		{"empty filter", "", "", false},
		{"valid type", "compute", "", false},
		{"valid status", "", "running", false},
		{"invalid type", "quantum", "", true},
		{"invalid status", "", "exploding", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := provider.ParseFilter("", tt.typ, "", tt.status)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFilter() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseFilter_LengthLimit(t *testing.T) {
	t.Parallel()
	longStr := make([]byte, 100)
	for i := range longStr {
		longStr[i] = 'a'
	}
	_, err := provider.ParseFilter(string(longStr), "", "", "")
	if err == nil {
		t.Error("expected error for overly long provider name")
	}
}

func TestResource_MatchesFilter(t *testing.T) {
	t.Parallel()
	r := provider.Resource{
		Type:   provider.TypeCompute,
		Region: "us-east-1",
		Status: provider.StatusRunning,
	}

	tests := []struct {
		name   string
		filter provider.ResourceFilter
		want   bool
	}{
		{"empty filter matches all", provider.ResourceFilter{}, true},
		{"matching type", provider.ResourceFilter{Type: provider.TypeCompute}, true},
		{"non-matching type", provider.ResourceFilter{Type: provider.TypeStorage}, false},
		{"matching region", provider.ResourceFilter{Region: "us-east-1"}, true},
		{"non-matching region", provider.ResourceFilter{Region: "eu-west-1"}, false},
		{"matching status", provider.ResourceFilter{Status: provider.StatusRunning}, true},
		{"non-matching status", provider.ResourceFilter{Status: provider.StatusStopped}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := r.MatchesFilter(tt.filter); got != tt.want {
				t.Errorf("MatchesFilter() = %v, want %v", got, tt.want)
			}
		})
	}
}
