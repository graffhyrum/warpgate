package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewModel(t *testing.T) {
	t.Parallel()
	m := newModel("http://localhost:8080")
	if m.baseURL != "http://localhost:8080" {
		t.Errorf("expected baseURL http://localhost:8080, got %s", m.baseURL)
	}
	if m.quitting {
		t.Error("new model should not be quitting")
	}
}

func TestUpdate_Quit(t *testing.T) {
	t.Parallel()
	m := newModel("http://localhost:8080")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	um := updated.(model)
	if !um.quitting {
		t.Error("expected quitting after q key")
	}
	if cmd == nil {
		t.Error("expected a quit command")
	}
}

func TestUpdate_ProvidersMsg(t *testing.T) {
	t.Parallel()
	m := newModel("http://localhost:8080")
	providers := []providerHealth{
		{Name: "aws", Healthy: true, Latency: "45ms"},
		{Name: "gcp", Healthy: true, Latency: "52ms"},
		{Name: "azure", Healthy: false, Latency: "60ms", Error: "down"},
	}
	updated, _ := m.Update(providersMsg{providers: providers})
	um := updated.(model)
	if len(um.providers) != 3 {
		t.Fatalf("expected 3 providers, got %d", len(um.providers))
	}
	if um.err != nil {
		t.Errorf("unexpected error: %v", um.err)
	}
	if um.lastRefresh.IsZero() {
		t.Error("lastRefresh should be set")
	}
}

func TestUpdate_ProvidersMsgError(t *testing.T) {
	t.Parallel()
	m := newModel("http://localhost:8080")
	updated, _ := m.Update(providersMsg{err: http.ErrServerClosed})
	um := updated.(model)
	if um.err == nil {
		t.Error("expected error to be set")
	}
	if um.providers != nil {
		t.Error("providers should be nil on error")
	}
}

func TestUpdate_ResourcesMsg(t *testing.T) {
	t.Parallel()
	m := newModel("http://localhost:8080")
	resources := []resource{
		{ID: "aws-ec2-001", Name: "web-server-1", Type: "compute", Provider: "aws", Region: "us-east-1", Status: "running"},
		{ID: "gcp-gce-001", Name: "api-server-1", Type: "compute", Provider: "gcp", Region: "us-central1", Status: "running"},
	}
	updated, _ := m.Update(resourcesMsg{resources: resources, count: 2, warnings: []string{}})
	um := updated.(model)
	if um.resCount != 2 {
		t.Errorf("expected 2 resources, got %d", um.resCount)
	}
	if len(um.table.Rows()) != 2 {
		t.Errorf("expected 2 table rows, got %d", len(um.table.Rows()))
	}
}

func TestUpdate_WindowSize(t *testing.T) {
	t.Parallel()
	m := newModel("http://localhost:8080")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	um := updated.(model)
	if um.width != 120 {
		t.Errorf("expected width 120, got %d", um.width)
	}
	if um.height != 40 {
		t.Errorf("expected height 40, got %d", um.height)
	}
}

func TestView_WithData(t *testing.T) {
	t.Parallel()
	m := newModel("http://localhost:8080")
	m.providers = []providerHealth{
		{Name: "aws", Healthy: true, Latency: "45ms"},
		{Name: "gcp", Healthy: false, Latency: "52ms", Error: "timeout"},
	}
	m.resources = []resource{
		{ID: "aws-ec2-001", Name: "web-server-1", Type: "compute", Provider: "aws", Region: "us-east-1", Status: "running"},
	}
	m.resCount = 1
	m.table.SetRows(resourcesToRows(m.resources))

	view := m.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(view, "warpgate dashboard") {
		t.Error("expected title in view")
	}
	if !strings.Contains(view, "aws") {
		t.Error("expected aws provider in view")
	}
}

func TestView_Quitting(t *testing.T) {
	t.Parallel()
	m := newModel("http://localhost:8080")
	m.quitting = true
	if m.View() != "" {
		t.Error("expected empty view when quitting")
	}
}

func TestView_WithError(t *testing.T) {
	t.Parallel()
	m := newModel("http://localhost:8080")
	m.err = http.ErrServerClosed
	view := m.View()
	if !strings.Contains(view, "http: Server closed") {
		t.Error("expected error message in view")
	}
}

func TestFetchProviders_Integration(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"providers": []map[string]any{
				{"name": "aws", "healthy": true, "latency": "10ms"},
			},
		})
	}))
	defer ts.Close()

	cmd := fetchProviders(ts.URL)
	msg := cmd()

	pm, ok := msg.(providersMsg)
	if !ok {
		t.Fatalf("expected providersMsg, got %T", msg)
	}
	if pm.err != nil {
		t.Fatalf("unexpected error: %v", pm.err)
	}
	if len(pm.providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(pm.providers))
	}
	if pm.providers[0].Name != "aws" {
		t.Errorf("expected provider name aws, got %s", pm.providers[0].Name)
	}
}

func TestFetchResources_Integration(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resources": []map[string]any{
				{"id": "a1", "name": "srv", "type": "compute", "provider": "aws", "region": "us-east-1", "status": "running"},
			},
			"count":    1,
			"warnings": []string{},
		})
	}))
	defer ts.Close()

	cmd := fetchResources(ts.URL)
	msg := cmd()

	rm, ok := msg.(resourcesMsg)
	if !ok {
		t.Fatalf("expected resourcesMsg, got %T", msg)
	}
	if rm.err != nil {
		t.Fatalf("unexpected error: %v", rm.err)
	}
	if rm.count != 1 {
		t.Errorf("expected count 1, got %d", rm.count)
	}
}

func TestFetchProviders_ConnectionError(t *testing.T) {
	t.Parallel()
	cmd := fetchProviders("http://127.0.0.1:1")
	msg := cmd()
	pm := msg.(providersMsg)
	if pm.err == nil {
		t.Error("expected connection error")
	}
}

func TestResourceBreakdown(t *testing.T) {
	t.Parallel()
	resources := []resource{
		{Type: "compute"}, {Type: "compute"}, {Type: "storage"},
	}
	result := resourceBreakdown(resources)
	if !strings.Contains(result, "2 compute") {
		t.Errorf("expected 2 compute in breakdown, got %s", result)
	}
	if !strings.Contains(result, "1 storage") {
		t.Errorf("expected 1 storage in breakdown, got %s", result)
	}
}

func TestResourcesToRows(t *testing.T) {
	t.Parallel()
	resources := []resource{
		{ID: "a1", Name: "srv", Type: "compute", Provider: "aws", Region: "us-east-1", Status: "running"},
	}
	rows := resourcesToRows(resources)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0][0] != "a1" {
		t.Errorf("expected first column a1, got %s", rows[0][0])
	}
}

