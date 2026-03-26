package mock_test

import (
	"context"
	"errors"
	"testing"

	"github.com/graffhyrum/warpgate/internal/mock"
	"github.com/graffhyrum/warpgate/internal/provider"
)

func TestDefaultProviders_Count(t *testing.T) {
	t.Parallel()
	providers := mock.DefaultProviders()
	if len(providers) != 3 {
		t.Fatalf("expected 3 default providers, got %d", len(providers))
	}

	names := make(map[string]bool)
	for _, p := range providers {
		names[p.Name()] = true
	}
	for _, expected := range []string{"aws", "gcp", "azure"} {
		if !names[expected] {
			t.Errorf("missing expected provider %q", expected)
		}
	}
}

func TestProvider_ListResources_FilterByType(t *testing.T) {
	t.Parallel()
	providers := mock.DefaultProviders()
	aws := providers[0] // aws

	resources, err := aws.ListResources(context.Background(), provider.ResourceFilter{Type: provider.TypeCompute})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range resources {
		if r.Type != provider.TypeCompute {
			t.Errorf("expected type compute, got %s for resource %s", r.Type, r.ID)
		}
	}
	if len(resources) == 0 {
		t.Error("expected at least one compute resource from aws")
	}
}

func TestProvider_ListResources_FilterByRegion(t *testing.T) {
	t.Parallel()
	providers := mock.DefaultProviders()
	aws := providers[0]

	resources, err := aws.ListResources(context.Background(), provider.ResourceFilter{Region: "us-east-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range resources {
		if r.Region != "us-east-1" {
			t.Errorf("expected region us-east-1, got %s for resource %s", r.Region, r.ID)
		}
	}
}

func TestProvider_GetResource_Found(t *testing.T) {
	t.Parallel()
	providers := mock.DefaultProviders()
	aws := providers[0]

	res, err := aws.GetResource(context.Background(), "aws-ec2-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Name != "web-server-1" {
		t.Errorf("expected name 'web-server-1', got %q", res.Name)
	}
}

func TestProvider_GetResource_NotFound(t *testing.T) {
	t.Parallel()
	providers := mock.DefaultProviders()
	aws := providers[0]

	_, err := aws.GetResource(context.Background(), "nonexistent")
	if !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestProvider_CheckHealth(t *testing.T) {
	t.Parallel()
	providers := mock.DefaultProviders()
	for _, p := range providers {
		hs := p.CheckHealth(context.Background())
		if !hs.Healthy {
			t.Errorf("default provider %q should be healthy", hs.Provider)
		}
		if hs.Latency <= 0 {
			t.Errorf("default provider %q should have positive latency", hs.Provider)
		}
	}
}
