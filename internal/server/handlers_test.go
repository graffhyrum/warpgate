package server_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/graffhyrum/warpgate/internal/mock"
	"github.com/graffhyrum/warpgate/internal/provider"
	"github.com/graffhyrum/warpgate/internal/server"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	reg := provider.NewRegistry(5 * time.Second)
	for _, p := range mock.DefaultProviders() {
		reg.Register(p)
	}
	srv := server.New(0, reg, slog.Default())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestHealth(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Request-Id") == "" {
		t.Error("expected X-Request-Id header")
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("expected X-Content-Type-Options: nosniff header")
	}
}

func TestListResources(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/v1/resources")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	resources, ok := body["resources"].([]any)
	if !ok {
		t.Fatal("expected resources array in response")
	}
	if len(resources) == 0 {
		t.Error("expected at least one resource")
	}
}

func TestListResources_FilterByProvider(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/v1/resources?provider=aws")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	resources := body["resources"].([]any)
	for _, r := range resources {
		rm := r.(map[string]any)
		if rm["provider"] != "aws" {
			t.Errorf("expected provider aws, got %s", rm["provider"])
		}
	}
}

func TestListResources_InvalidFilter(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/v1/resources?type=quantum")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetResource_NotFound(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/v1/resources/nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["error"] != "resource not found" {
		t.Errorf("expected 'resource not found' error, got %v", body["error"])
	}
	if body["request_id"] == nil || body["request_id"] == "" {
		t.Error("expected request_id in error response")
	}
}

func TestGetResource_Found(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/v1/resources/aws-ec2-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["id"] != "aws-ec2-001" {
		t.Errorf("expected id aws-ec2-001, got %v", body["id"])
	}
}

func TestListProviders(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/v1/providers")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	providers := body["providers"].([]any)
	if len(providers) != 3 {
		t.Errorf("expected 3 providers, got %d", len(providers))
	}
}

func TestReady_NoProviders(t *testing.T) {
	t.Parallel()
	reg := provider.NewRegistry(5 * time.Second)
	srv := server.New(0, reg, slog.Default())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/ready")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 with no providers, got %d", resp.StatusCode)
	}
}
