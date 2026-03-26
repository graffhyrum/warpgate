// Package mock provides configurable mock infrastructure providers
// with cloud-realistic data for AWS, GCP, and Azure.
package mock

import (
	"context"
	"time"

	"github.com/graffhyrum/warpgate/internal/provider"
)

// Provider is a mock infrastructure provider with static resource data.
type Provider struct {
	name      string
	resources []provider.Resource
	healthy   bool
	latency   time.Duration
}

// Config holds initialization parameters for a mock provider.
type Config struct {
	Name      string
	Resources []provider.Resource
	Healthy   bool
	Latency   time.Duration
}

// New creates a mock provider from config.
func New(cfg Config) *Provider {
	return &Provider{
		name:      cfg.Name,
		resources: cfg.Resources,
		healthy:   cfg.Healthy,
		latency:   cfg.Latency,
	}
}

func (p *Provider) Name() string { return p.name }

func (p *Provider) ListResources(_ context.Context, filter provider.ResourceFilter) ([]provider.Resource, error) {
	var result []provider.Resource
	for _, r := range p.resources {
		if r.MatchesFilter(filter) {
			result = append(result, r)
		}
	}
	if result == nil {
		result = []provider.Resource{}
	}
	return result, nil
}

func (p *Provider) GetResource(_ context.Context, id string) (*provider.Resource, error) {
	for _, r := range p.resources {
		if r.ID == id {
			// Return a copy to prevent aliasing into internal slice
			res := r
			return &res, nil
		}
	}
	return nil, provider.ErrNotFound
}

func (p *Provider) CheckHealth(_ context.Context) provider.HealthStatus {
	hs := provider.HealthStatus{
		Provider: p.name,
		Healthy:  p.healthy,
		Latency:  p.latency,
	}
	if !p.healthy {
		hs.Error = p.name + " is unhealthy"
	}
	return hs
}

// DefaultProviders returns three mock providers seeded with cloud-realistic data.
func DefaultProviders() []*Provider {
	return []*Provider{
		New(Config{
			Name:    "aws",
			Healthy: true,
			Latency: 45 * time.Millisecond,
			Resources: []provider.Resource{
				{ID: "aws-ec2-001", Name: "web-server-1", Type: provider.TypeCompute, Provider: "aws", Region: "us-east-1", Status: provider.StatusRunning, Tags: tags("env", "prod", "team", "platform")},
				{ID: "aws-ec2-002", Name: "web-server-2", Type: provider.TypeCompute, Provider: "aws", Region: "us-west-2", Status: provider.StatusRunning, Tags: tags("env", "prod", "team", "platform")},
				{ID: "aws-s3-001", Name: "assets-bucket", Type: provider.TypeStorage, Provider: "aws", Region: "us-east-1", Status: provider.StatusRunning, Tags: tags("env", "prod")},
				{ID: "aws-rds-001", Name: "primary-db", Type: provider.TypeDatabase, Provider: "aws", Region: "us-east-1", Status: provider.StatusRunning, Tags: tags("env", "prod", "engine", "postgres")},
				{ID: "aws-vpc-001", Name: "prod-vpc", Type: provider.TypeNetwork, Provider: "aws", Region: "us-east-1", Status: provider.StatusRunning},
				{ID: "aws-ec2-003", Name: "batch-worker", Type: provider.TypeCompute, Provider: "aws", Region: "eu-west-1", Status: provider.StatusStopped, Tags: tags("env", "staging")},
			},
		}),
		New(Config{
			Name:    "gcp",
			Healthy: true,
			Latency: 52 * time.Millisecond,
			Resources: []provider.Resource{
				{ID: "gcp-gce-001", Name: "api-server-1", Type: provider.TypeCompute, Provider: "gcp", Region: "us-central1", Status: provider.StatusRunning, Tags: tags("env", "prod")},
				{ID: "gcp-gcs-001", Name: "data-lake", Type: provider.TypeStorage, Provider: "gcp", Region: "us-central1", Status: provider.StatusRunning, Tags: tags("env", "prod")},
				{ID: "gcp-sql-001", Name: "analytics-db", Type: provider.TypeDatabase, Provider: "gcp", Region: "europe-west1", Status: provider.StatusDegraded, Tags: tags("env", "prod", "engine", "mysql")},
				{ID: "gcp-vpc-001", Name: "default-network", Type: provider.TypeNetwork, Provider: "gcp", Region: "us-central1", Status: provider.StatusRunning},
			},
		}),
		New(Config{
			Name:    "azure",
			Healthy: true,
			Latency: 60 * time.Millisecond,
			Resources: []provider.Resource{
				{ID: "az-vm-001", Name: "auth-server", Type: provider.TypeCompute, Provider: "azure", Region: "eastus", Status: provider.StatusRunning, Tags: tags("env", "prod")},
				{ID: "az-blob-001", Name: "backups", Type: provider.TypeStorage, Provider: "azure", Region: "westeurope", Status: provider.StatusRunning, Tags: tags("env", "prod")},
				{ID: "az-sql-001", Name: "user-db", Type: provider.TypeDatabase, Provider: "azure", Region: "eastus", Status: provider.StatusRunning, Tags: tags("env", "prod", "engine", "sqlserver")},
				{ID: "az-vnet-001", Name: "corp-network", Type: provider.TypeNetwork, Provider: "azure", Region: "eastus", Status: provider.StatusRunning},
				{ID: "az-vm-002", Name: "legacy-app", Type: provider.TypeCompute, Provider: "azure", Region: "westeurope", Status: provider.StatusStopped, Tags: tags("env", "decom")},
			},
		}),
	}
}

func tags(pairs ...string) map[string]string {
	m := make(map[string]string, len(pairs)/2)
	for i := 0; i < len(pairs)-1; i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return m
}
