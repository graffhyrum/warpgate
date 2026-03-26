// Package main is the entry point for the warpgate TUI dashboard.
// It connects to a running warpgate server and displays live provider
// health, resource inventory, and request metrics.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const defaultBaseURL = "http://localhost:8080"

func main() {
	baseURL := defaultBaseURL
	if len(os.Args) > 1 {
		baseURL = os.Args[1]
	}

	p := tea.NewProgram(
		newModel(baseURL),
		tea.WithAltScreen(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// --- Messages ---

type tickMsg time.Time

type providersMsg struct {
	providers []providerHealth
	err       error
}

type resourcesMsg struct {
	resources []resource
	warnings  []string
	count     int
	err       error
}

// --- API types ---

type providerHealth struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	Latency string `json:"latency"`
	Error   string `json:"error,omitempty"`
}

type resource struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Provider string `json:"provider"`
	Region   string `json:"region"`
	Status   string `json:"status"`
}

// --- Model ---

type model struct {
	baseURL     string
	providers   []providerHealth
	resources   []resource
	warnings    []string
	resCount    int
	table       table.Model
	lastRefresh time.Time
	err         error
	quitting    bool
	width       int
	height      int
}

func newModel(baseURL string) model {
	cols := []table.Column{
		{Title: "ID", Width: 14},
		{Title: "Name", Width: 18},
		{Title: "Type", Width: 10},
		{Title: "Provider", Width: 8},
		{Title: "Region", Width: 14},
		{Title: "Status", Width: 10},
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithRows([]table.Row{}),
		table.WithHeight(15),
	)
	t.Focus()
	t.SetStyles(tableStyles())

	return model{
		baseURL: baseURL,
		table:   t,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		fetchProviders(m.baseURL),
		fetchResources(m.baseURL),
		tick(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.SetHeight(max(msg.Height-16, 5))
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "r":
			return m, tea.Batch(fetchProviders(m.baseURL), fetchResources(m.baseURL))
		}

	case tickMsg:
		return m, tea.Batch(
			fetchProviders(m.baseURL),
			fetchResources(m.baseURL),
			tick(),
		)

	case providersMsg:
		if msg.err != nil {
			m.err = msg.err
			m.providers = nil
		} else {
			m.err = nil
			m.providers = msg.providers
			m.lastRefresh = time.Now()
		}
		return m, nil

	case resourcesMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.resources = msg.resources
			m.warnings = msg.warnings
			m.resCount = msg.count
			m.table.SetRows(resourcesToRows(msg.resources))
			m.lastRefresh = time.Now()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Title bar
	b.WriteString(titleStyle.Render(" ⎔ warpgate dashboard "))
	b.WriteString("\n\n")

	// Error banner
	if m.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf(" ✗ %s ", m.err)))
		b.WriteString("\n\n")
	}

	// Provider health panel
	b.WriteString(sectionStyle.Render("Providers"))
	b.WriteString("\n")
	if len(m.providers) == 0 {
		b.WriteString(dimStyle.Render("  waiting for data..."))
	} else {
		for _, p := range m.providers {
			b.WriteString(renderProvider(p))
		}
	}
	b.WriteString("\n\n")

	// Warnings
	if len(m.warnings) > 0 {
		b.WriteString(warnStyle.Render(fmt.Sprintf(" ⚠ %d warning(s): %s ",
			len(m.warnings), strings.Join(m.warnings, "; "))))
		b.WriteString("\n\n")
	}

	// Resource summary
	summary := fmt.Sprintf("Resources: %d total", m.resCount)
	if len(m.resources) > 0 {
		summary += " │ " + resourceBreakdown(m.resources)
	}
	b.WriteString(sectionStyle.Render(summary))
	b.WriteString("\n")

	// Resource table
	b.WriteString(m.table.View())
	b.WriteString("\n\n")

	// Status bar
	refreshed := "never"
	if !m.lastRefresh.IsZero() {
		refreshed = m.lastRefresh.Format("15:04:05")
	}
	b.WriteString(statusStyle.Render(fmt.Sprintf(
		" %s │ refreshed %s │ q quit │ r refresh │ ↑↓ navigate ",
		m.baseURL, refreshed,
	)))

	return b.String()
}

// --- Commands ---

func tick() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

var httpClient = &http.Client{Timeout: 5 * time.Second}

func fetchProviders(baseURL string) tea.Cmd {
	return func() tea.Msg {
		resp, err := httpClient.Get(baseURL + "/api/v1/providers")
		if err != nil {
			return providersMsg{err: fmt.Errorf("connect: %s", err)}
		}
		defer resp.Body.Close()

		var body struct {
			Providers []providerHealth `json:"providers"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return providersMsg{err: fmt.Errorf("decode: %s", err)}
		}
		sort.Slice(body.Providers, func(i, j int) bool {
			return body.Providers[i].Name < body.Providers[j].Name
		})
		return providersMsg{providers: body.Providers}
	}
}

func fetchResources(baseURL string) tea.Cmd {
	return func() tea.Msg {
		resp, err := httpClient.Get(baseURL + "/api/v1/resources")
		if err != nil {
			return resourcesMsg{err: fmt.Errorf("connect: %s", err)}
		}
		defer resp.Body.Close()

		var body struct {
			Resources []resource `json:"resources"`
			Warnings  []string   `json:"warnings"`
			Count     int        `json:"count"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return resourcesMsg{err: fmt.Errorf("decode: %s", err)}
		}
		sort.Slice(body.Resources, func(i, j int) bool {
			if body.Resources[i].Provider != body.Resources[j].Provider {
				return body.Resources[i].Provider < body.Resources[j].Provider
			}
			return body.Resources[i].ID < body.Resources[j].ID
		})
		return resourcesMsg{
			resources: body.Resources,
			warnings:  body.Warnings,
			count:     body.Count,
		}
	}
}

// --- Helpers ---

func resourcesToRows(resources []resource) []table.Row {
	rows := make([]table.Row, len(resources))
	for i, r := range resources {
		rows[i] = table.Row{r.ID, r.Name, r.Type, r.Provider, r.Region, r.Status}
	}
	return rows
}

func renderProvider(p providerHealth) string {
	indicator := healthyStyle.Render("●")
	if !p.Healthy {
		indicator = unhealthyStyle.Render("●")
	}
	name := providerNameStyle.Render(p.Name)
	latency := dimStyle.Render(p.Latency)
	line := fmt.Sprintf("  %s %s %s", indicator, name, latency)
	if p.Error != "" {
		line += " " + errorTextStyle.Render(p.Error)
	}
	return line + "\n"
}

func resourceBreakdown(resources []resource) string {
	byType := make(map[string]int)
	for _, r := range resources {
		byType[r.Type]++
	}
	parts := make([]string, 0, len(byType))
	for t, n := range byType {
		parts = append(parts, fmt.Sprintf("%d %s", n, t))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// --- Styles ---

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#5B44E0"))

	sectionStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA"))

	healthyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575"))

	unhealthyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF4672"))

	providerNameStyle = lipgloss.NewStyle().
				Bold(true).
				Width(8)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#FF4672"))

	errorTextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF4672"))

	warnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1a1a1a")).
			Background(lipgloss.Color("#FFBA08"))

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))
)

func tableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	return s
}
