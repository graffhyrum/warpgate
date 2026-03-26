# SPIKE: TUI HTTP Client Extraction -- Shared API Contract

**Bead**: warpgate-7ux
**Date**: 2026-03-26
**Timebox**: 2 hours
**Status**: Complete

---

## 1. Current State

The TUI (`cmd/warpgate-tui/main.go`, 385 lines) breaks down as:

| Concern | Lines | % |
|---|---|---|
| HTTP plumbing (`fetchProviders`, `fetchResources`, `httpClient` var, inline anonymous response structs) | ~55 | 14% |
| API types (`providerHealth`, `resource`) | ~16 | 4% |
| Bubble Tea model + Update + Init | ~60 | 16% |
| View rendering | ~60 | 16% |
| Helpers (`resourcesToRows`, `renderProvider`, `resourceBreakdown`) | ~35 | 9% |
| Styles | ~55 | 14% |
| Msg types, tick, main | ~30 | 8% |

The TUI defines its own types to decode server JSON:

- `providerHealth` -- mirrors `server.providerView` exactly (same JSON tags, same fields).
- `resource` -- mirrors `provider.Resource` but uses `string` for `Type`/`Status`/`Tags` instead of `ResourceType`/`ResourceStatus`/`map[string]string`. The `Tags` field is omitted entirely.

HTTP calls are two functions (`fetchProviders`, `fetchResources`) that return `tea.Cmd` closures. Each does GET, decode JSON into an anonymous wrapper struct, sort results, and return a typed message.

## 2. API Contract Gap

The server constructs JSON responses via `map[string]any` literals in handlers.go -- there are no named response structs for the list endpoints. The response shape is implicit:

```go
// handleListProviders
map[string]any{"providers": toProviderViews(results)}

// handleListResources
map[string]any{
    "resources": result.Resources,
    "count":     len(result.Resources),
    "warnings":  warnings,
}
```

The TUI decodes into its own anonymous structs and local types. The only shared contract is "both sides agree on the JSON field names." Drift is caught only at runtime -- e.g., if the server renamed `"providers"` to `"data"`, the TUI would silently get zero results with no error.

`providerView` (handlers.go:89-94) is the closest thing to a named response type on the server side, but it lives in the `server` package and the TUI cannot import it (circular dependency: `server` imports `provider`, and the TUI would import both).

## 3. Options

### Option A -- Shared Client Package

Create `internal/client/client.go`:

```go
package client

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"
)

// ProviderHealth is the API representation of a provider's health status.
type ProviderHealth struct {
    Name    string `json:"name"`
    Healthy bool   `json:"healthy"`
    Latency string `json:"latency"`
    Error   string `json:"error,omitempty"`
}

// Resource is the API representation of an infrastructure resource.
type Resource struct {
    ID       string `json:"id"`
    Name     string `json:"name"`
    Type     string `json:"type"`
    Provider string `json:"provider"`
    Region   string `json:"region"`
    Status   string `json:"status"`
}

// Client defines the operations available against a warpgate server.
type Client interface {
    ListProviders(ctx context.Context) ([]ProviderHealth, error)
    ListResources(ctx context.Context) ([]Resource, error)
}

// HTTPClient is the concrete implementation that talks to a real server.
type HTTPClient struct {
    BaseURL    string
    HTTP       *http.Client
}

func New(baseURL string) *HTTPClient {
    return &HTTPClient{
        BaseURL: baseURL,
        HTTP:    &http.Client{Timeout: 5 * time.Second},
    }
}

func (c *HTTPClient) ListProviders(ctx context.Context) ([]ProviderHealth, error) {
    // GET /api/v1/providers, decode, return
}

func (c *HTTPClient) ListResources(ctx context.Context) ([]Resource, error) {
    // GET /api/v1/resources, decode, return
}
```

**Impact on TUI**: Replace `fetchProviders`/`fetchResources` with calls through the `Client` interface. The TUI model holds a `Client` field instead of `baseURL`. Msg types carry `[]client.ProviderHealth` and `[]client.Resource` instead of local types.

**Impact on server**: The server's `handleListProviders` and `handleListResources` could encode `client.ProviderHealth` / `client.Resource` directly instead of `map[string]any` + `providerView`. The `providerView` type moves to `client.ProviderHealth`. Import direction: `server` imports `internal/client` for types only (no circular dependency since `client` does not import `server`).

**Testing**: The `Client` interface enables a stub:

```go
type StubClient struct {
    Providers []client.ProviderHealth
    Resources []client.Resource
    Err       error
}

func (s *StubClient) ListProviders(ctx context.Context) ([]client.ProviderHealth, error) {
    return s.Providers, s.Err
}

func (s *StubClient) ListResources(ctx context.Context) ([]client.Resource, error) {
    return s.Resources, s.Err
}
```

The TUI tests currently construct `providersMsg{providers: ...}` directly -- this works but does not exercise the HTTP layer. With Option A, tests can inject `StubClient` to test the full Update cycle without `httptest.NewServer`.

### Option B -- Shared Types Only

Create `internal/api/types.go` with just the response types (no client logic). The TUI keeps its own HTTP calls but imports shared types:

```go
package api

type ProviderHealth struct { /* same fields */ }
type Resource struct { /* same fields */ }
```

**Pros**: Minimal change. Eliminates type drift. Server and TUI both import `internal/api`.

**Cons**: No testability improvement -- the TUI still has inline HTTP calls and `tea.Cmd` closures. Tests still need `httptest.NewServer` to exercise fetch logic.

### Option C -- Keep As-Is + Integration Test

No extraction. Add an integration test that starts the real server and verifies the TUI can decode its responses.

**Pros**: Zero refactoring. Validates the contract end-to-end.

**Cons**: Slow test (server startup). Does not improve TUI testability or code organization. Type drift remains latent between test runs.

## 4. Interface Design (Option A)

```go
// Client defines the operations available against a warpgate server.
type Client interface {
    ListProviders(ctx context.Context) ([]ProviderHealth, error)
    ListResources(ctx context.Context) ([]Resource, error)
}
```

This is the minimal surface. `GetResource` can be added later if the TUI needs a detail view.

The interface is small enough that table-driven test stubs are trivial. A mock generated by `go generate` would be overkill for two methods.

## 5. Type Sharing Strategy

**Recommended location**: `internal/client/`

Rationale: The types describe the *client-facing API contract* -- they are JSON DTOs, not domain types. `internal/api/` is also reasonable but `internal/client/` makes the package self-contained (types + HTTP implementation + interface).

**Import direction**:

```
cmd/warpgate-tui  -->  internal/client  (interface + types + HTTP impl)
internal/server   -->  internal/client  (types only, for encoding)
internal/server   -->  internal/provider (domain types)
internal/client   -->  (nothing internal)
```

No circular dependencies. The `internal/client` package is a leaf.

**Type coercion**: The server currently encodes `provider.Resource` directly (which has `ResourceType` and `ResourceStatus` typed fields). The JSON output is `string` because these types have `String()` via their underlying type. The client types use plain `string` -- no information is lost in the JSON round-trip. The server can either continue encoding `provider.Resource` directly (JSON is compatible) or convert to `client.Resource` explicitly for tighter contract control.

## 6. Recommendation

**Option A -- Shared Client Package** is the right choice.

**Portfolio value**: Demonstrates:
- Interface-based design for testability (Go best practice)
- Clean package boundaries with unidirectional dependencies
- Elimination of type duplication across package boundaries
- Separation of I/O (HTTP) from presentation (Bubble Tea)

**Proportionality**: The extraction touches ~70 lines in the TUI (remove local types + rewrite two fetch functions) and adds ~80 lines in the new package. Net code stays roughly flat. The server change is optional (can keep `map[string]any` encoding and just delete `providerView` in a follow-up).

**Suggested implementation order**:

1. Create `internal/client/` with types, interface, and `HTTPClient`
2. Add `internal/client/client_test.go` with `httptest.NewServer`-based tests
3. Refactor TUI to accept `Client` interface; delete local types and fetch functions
4. Update TUI tests to use `StubClient`
5. (Optional) Update server handlers to encode `client.ProviderHealth` instead of `providerView`

Each step is a standalone commit that passes `go build` and `go test`.
