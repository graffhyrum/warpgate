# SPIKE: Registry Responsibility Split -- Management vs Orchestration

**Bead**: warpgate-7pb
**Date**: 2026-03-26
**Timebox**: 2 hours
**Status**: Complete

## 1. Current State

`internal/provider/registry.go` defines a single `Registry` struct that serves two distinct roles:

### Provider Management (CRUD over a map)

| Method        | Responsibility                          |
| ------------- | --------------------------------------- |
| `Register(p)` | Insert provider into `map[string]Provider` |
| `Provider(name)` | Lookup by name                       |
| `Providers()`  | List registered names                  |
| `snapshotProviders()` | Copy map for safe concurrent iteration |

These methods are thin wrappers around a `sync.RWMutex`-protected map. They contain no business logic beyond locking.

### Query Orchestration (fan-out/fan-in with timeouts and error aggregation)

| Method          | Responsibility                                                        |
| --------------- | --------------------------------------------------------------------- |
| `ListAll()`     | Fan-out `ListResources` to N providers, aggregate results + warnings  |
| `GetResource()` | Fan-out `GetResource` to N providers, first-match wins, error triage  |
| `CheckHealth()` | Fan-out `CheckHealth` to N providers, collect results                 |

These methods share a common pattern:
1. `snapshotProviders()` -- copy the map to release the lock
2. Create `errgroup.Group`
3. For each provider, launch a goroutine with `context.WithTimeout(ctx, r.timeout)`
4. Collect results under a local `sync.Mutex`
5. `g.Wait()`, return aggregated result

The shared orchestration skeleton is duplicated three times with minor variations:
- **ListAll**: partial failure becomes warnings, results are `[]Resource`
- **GetResource**: first-match short-circuit semantics, distinguishes `ErrNotFound` from real errors
- **CheckHealth**: always succeeds (no error path), results are `[]HealthStatus`

## 2. Proposed Split

### ProviderStore -- registration and lookup

```go
// ProviderStore manages the set of registered providers.
// It is safe for concurrent use.
type ProviderStore struct {
    mu        sync.RWMutex
    providers map[string]Provider
}

func NewProviderStore() *ProviderStore

// Register adds or replaces a provider.
func (s *ProviderStore) Register(p Provider)

// Get returns a provider by name, or nil if not found.
func (s *ProviderStore) Get(name string) Provider

// Names returns the names of all registered providers.
func (s *ProviderStore) Names() []string

// Snapshot returns a point-in-time copy of all providers.
// Callers may iterate without holding the lock.
func (s *ProviderStore) Snapshot() []Provider
```

`ProviderStore` owns the mutex and the map. It has no timeout, no context, no concurrency fan-out. Tests are trivial: register, lookup, list names, snapshot isolation.

### QueryOrchestrator -- fan-out execution

```go
// QueryOrchestrator fans out queries across providers with
// per-provider timeouts and concurrent execution.
type QueryOrchestrator struct {
    store   *ProviderStore
    timeout time.Duration
}

func NewQueryOrchestrator(store *ProviderStore, timeout time.Duration) *QueryOrchestrator

func (o *QueryOrchestrator) ListAll(ctx context.Context, filter ResourceFilter) ListResult
func (o *QueryOrchestrator) GetResource(ctx context.Context, id string) (*Resource, error)
func (o *QueryOrchestrator) CheckHealth(ctx context.Context) []HealthStatus
```

The orchestrator depends on `ProviderStore` for its `Snapshot()` method. It owns the timeout, the `errgroup` fan-out, and the result aggregation logic.

### Alternative: Slice-based dependency

Instead of depending on `*ProviderStore`, the orchestrator could accept a `[]Provider` slice directly:

```go
func (o *QueryOrchestrator) ListAll(ctx context.Context, providers []Provider, filter ResourceFilter) ListResult
```

**Rejected.** This pushes snapshot-timing responsibility to every caller. The orchestrator should own the decision of when to snapshot, keeping the API simple and the lock duration predictable.

## 3. Dependency Direction

```
cmd/main.go
    |
    +---> ProviderStore    (owns provider map)
    |         ^
    |         |
    +---> QueryOrchestrator (depends on ProviderStore)
    |         ^
    |         |
    +---> server.Server    (depends on orchestrator interface)
```

The server package already defines its own `Registry` interface (in `server.go:16-21`) with only the orchestration methods plus `Providers()`. After the split, the server interface becomes:

```go
type Registry interface {
    ListAll(ctx context.Context, filter provider.ResourceFilter) provider.ListResult
    GetResource(ctx context.Context, id string) (*provider.Resource, error)
    CheckHealth(ctx context.Context) []provider.HealthStatus
    Providers() []string
}
```

This interface is satisfied by `QueryOrchestrator` if we add a `Providers()` method that delegates to the store. No change needed in the server package -- the interface already describes exactly the orchestration surface.

## 4. Test Impact

### Current test inventory (registry_test.go)

| Test                                        | Category after split |
| ------------------------------------------- | -------------------- |
| `TestRegistry_ListAll_FanOut`                | Orchestration        |
| `TestRegistry_ListAll_PartialFailure`        | Orchestration        |
| `TestRegistry_ListAll_SingleProvider`         | Orchestration        |
| `TestRegistry_ListAll_UnknownProvider`        | Orchestration        |
| `TestRegistry_GetResource_Found`             | Orchestration        |
| `TestRegistry_GetResource_NotFound`          | Orchestration        |
| `TestRegistry_GetResource_DistinguishesRealErrors` | Orchestration  |
| `TestRegistry_CheckHealth`                   | Orchestration        |
| `TestParseFilter` / `TestParseFilter_LengthLimit` | Filter (unchanged) |
| `TestResource_MatchesFilter`                 | Resource (unchanged) |

**Observation**: Every Registry test exercises orchestration. There are zero tests for pure store behavior (register idempotency, concurrent register/lookup, snapshot isolation). This confirms the store responsibilities are undertested today.

### After split

**`store_test.go`** (new, trivial):
- Register + Get returns provider
- Register overwrites existing
- Get returns nil for unknown
- Names returns all registered names
- Snapshot returns independent copy (mutation-safe)
- Concurrent Register/Get under `t.Parallel()` with `-race`

**`orchestrator_test.go`** (renamed from registry_test.go):
- All existing orchestration tests, using a pre-populated `ProviderStore`
- New: timeout behavior (slow provider stub + short timeout = warning)
- New: context cancellation propagates to providers

## 5. Orchestration Pattern Analysis

The three fan-out methods share ~80% of their structure:

```
snapshot -> for each provider -> goroutine(timeout, call, collect) -> wait -> return
```

Differences:
- **Result type**: `[]Resource` vs `*Resource` vs `[]HealthStatus`
- **Error handling**: warnings (ListAll) vs error-triage (GetResource) vs ignore (CheckHealth)
- **Early exit**: GetResource could cancel remaining goroutines on first match (not currently implemented -- potential optimization)

A generic `fanOut[T]` helper could deduplicate this, but Go's type parameter constraints make the error-handling variations awkward. The three methods are short enough (20-25 lines each) that the duplication is acceptable and each method remains independently readable.

**Potential optimization for GetResource**: Use a cancellable context that is cancelled once `found != nil`. Currently all providers run to completion even after a match. This is a separate enhancement, not part of the split.

## 6. Recommendation

### Is the split worth it?

**Yes, with caveats.**

**Arguments for:**
- **Single Responsibility Principle**: The store is a data structure; the orchestrator is a coordination pattern. They change for different reasons (new provider registration semantics vs new fan-out strategies).
- **Testability**: Store tests become trivial and fast. Orchestration tests can use a pre-built store, isolating the fan-out logic.
- **Extensibility**: Adding provider priorities, weighted routing, circuit breakers, or rate limiting would live cleanly in the orchestrator without touching the store.
- **Portfolio signal**: Demonstrates awareness of SRP and interface segregation in Go -- relevant for a cloud infrastructure role where registries and orchestrators are common patterns.

**Arguments against:**
- **Project size**: Warpgate has 3 mock providers and ~186 lines of registry code. The split adds a file and moves code around without changing behavior. Over-engineering risk.
- **Indirection cost**: Two types where one worked fine. A reader now needs to understand the relationship between store and orchestrator.

**Verdict**: Do the split. The current Registry is the most interesting code in the project (concurrent fan-out, error aggregation, partial failure). Separating concerns makes it easier to discuss in an interview: "The store is boring by design; the orchestrator is where the concurrency lives." The cost is ~30 minutes of mechanical refactoring with no behavioral changes and no risk to existing tests.

### Implementation plan (if approved)

1. Create `internal/provider/store.go` with `ProviderStore`
2. Create `internal/provider/orchestrator.go` with `QueryOrchestrator`
3. Move methods from `registry.go`, preserving all behavior
4. Update `cmd/warpgate/main.go` to create store then orchestrator
5. Split `registry_test.go` into `store_test.go` and `orchestrator_test.go`
6. Add missing store tests (concurrent access, snapshot isolation)
7. Delete `registry.go` (or keep as a thin facade -- not recommended)
8. `make lint && go test ./...` -- zero behavior change expected
