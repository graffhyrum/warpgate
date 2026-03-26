// Package provider defines the core interface for infrastructure providers
// and the types shared across the warpgate system.
package provider

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ResourceType classifies infrastructure resources.
type ResourceType string

const (
	TypeCompute  ResourceType = "compute"
	TypeStorage  ResourceType = "storage"
	TypeNetwork  ResourceType = "network"
	TypeDatabase ResourceType = "database"
)

var knownTypes = map[ResourceType]bool{
	TypeCompute: true, TypeStorage: true,
	TypeNetwork: true, TypeDatabase: true,
}

// ResourceStatus represents the operational state of a resource.
type ResourceStatus string

const (
	StatusRunning  ResourceStatus = "running"
	StatusStopped  ResourceStatus = "stopped"
	StatusDegraded ResourceStatus = "degraded"
	StatusUnknown  ResourceStatus = "unknown"
)

var knownStatuses = map[ResourceStatus]bool{
	StatusRunning: true, StatusStopped: true,
	StatusDegraded: true, StatusUnknown: true,
}

// ErrNotFound is returned when a resource ID does not exist.
var ErrNotFound = errors.New("resource not found")

// Resource represents a single infrastructure resource.
type Resource struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     ResourceType      `json:"type"`
	Provider string            `json:"provider"`
	Region   string            `json:"region"`
	Status   ResourceStatus    `json:"status"`
	Tags     map[string]string `json:"tags,omitempty"`
}

// MatchesFilter returns true if the resource satisfies the given filter criteria.
func (r Resource) MatchesFilter(f ResourceFilter) bool {
	if f.Type != "" && r.Type != f.Type {
		return false
	}
	if f.Region != "" && r.Region != f.Region {
		return false
	}
	if f.Status != "" && r.Status != f.Status {
		return false
	}
	return true
}

// HealthStatus reports the health of a single provider.
type HealthStatus struct {
	Provider string        `json:"provider"`
	Healthy  bool          `json:"healthy"`
	Latency  time.Duration `json:"-"`
	Error    string        `json:"error,omitempty"`
}

const maxFilterFieldLen = 64

// ResourceFilter constrains which resources are returned.
type ResourceFilter struct {
	Provider string
	Type     ResourceType
	Region   string
	Status   ResourceStatus
}

// ParseFilter constructs a validated ResourceFilter from raw query strings.
// Returns an error suitable for a 400 response if any value is invalid.
func ParseFilter(provider, typ, region, status string) (ResourceFilter, error) {
	if len(provider) > maxFilterFieldLen {
		return ResourceFilter{}, fmt.Errorf("provider name too long (max %d)", maxFilterFieldLen)
	}
	if len(region) > maxFilterFieldLen {
		return ResourceFilter{}, fmt.Errorf("region name too long (max %d)", maxFilterFieldLen)
	}
	f := ResourceFilter{
		Provider: provider,
		Type:     ResourceType(typ),
		Region:   region,
		Status:   ResourceStatus(status),
	}
	if err := f.validate(); err != nil {
		return ResourceFilter{}, err
	}
	return f, nil
}

// validate checks that Type and Status, if set, are known constants.
func (f ResourceFilter) validate() error {
	if f.Type != "" && !knownTypes[f.Type] {
		return errors.New("unknown resource type: " + string(f.Type))
	}
	if f.Status != "" && !knownStatuses[f.Status] {
		return errors.New("unknown resource status: " + string(f.Status))
	}
	return nil
}

// Provider is the interface that infrastructure backends implement.
type Provider interface {
	Name() string
	ListResources(ctx context.Context, filter ResourceFilter) ([]Resource, error)
	GetResource(ctx context.Context, id string) (*Resource, error)
	CheckHealth(ctx context.Context) HealthStatus
}
