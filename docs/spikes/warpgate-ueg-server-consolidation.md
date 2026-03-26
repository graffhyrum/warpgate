# SPIKE: Server Package Shallow Module Consolidation

**Bead**: warpgate-ueg
**Date**: 2026-03-26
**Timebox**: 2 hours
**Status**: Complete

## 1. Current State

The `internal/server` package is split across three source files (plus one test file):

| File | Lines | Responsibility |
|------|-------|---------------|
| `server.go` | 80 | `Server` struct, `New()` constructor, `Start()`, `Shutdown()`, `Handler()`, local `Registry` interface |
| `handlers.go` | 127 | 5 HTTP handler methods on `Server`, JSON helpers (`writeJSON`, `writeError`), view types (`providerView`) |
| `middleware.go` | 114 | 4 middleware functions, `statusWriter` wrapper, `RequestID()` context accessor, context key type |
| `handlers_test.go` | 203 | 7 integration-style tests via `httptest.Server` |

**Total production code**: 321 lines across 3 files.

### Key observations

1. **Registry interface redeclaration**: `server.Registry` (4 methods) mirrors exactly the public method set of `provider.Registry` that the server actually uses: `ListAll`, `GetResource`, `CheckHealth`, `Providers`. The concrete `*provider.Registry` already satisfies this interface.

2. **Middleware functions are pure plumbing**: All four middleware functions (`requestIDMiddleware`, `securityHeadersMiddleware`, `loggingMiddleware`, `recoveryMiddleware`) are unexported and only composed inside `New()`. No external consumer calls them directly.

3. **Handlers are thin dispatchers**: Each handler method on `Server` does request parsing, calls `s.registry.<Method>()`, and writes a JSON response. The heaviest logic is `handleGetResource` with its 3-way error dispatch (not found / provider error / success).

4. **`statusWriter` is only used by middleware**: It is unexported and referenced only by `loggingMiddleware` and `recoveryMiddleware`.

## 2. Analysis: Ousterhout Depth Ratio

John Ousterhout's "A Philosophy of Software Design" defines **depth** as the ratio of functionality hidden behind an interface to the complexity of that interface. A **shallow module** has a complex interface relative to the functionality it provides.

The server package's public API:

```go
type Server struct { /* unexported fields */ }
func New(port int, registry Registry, logger *slog.Logger) *Server
func (s *Server) Start() error
func (s *Server) Handler() http.Handler
func (s *Server) Shutdown(ctx context.Context) error
func RequestID(ctx context.Context) string  // exported for handler use
```

This is a **5-symbol public API** hiding 321 lines of implementation (routes, middleware chain, JSON encoding, error mapping, panic recovery, status tracking). That is a healthy depth ratio -- roughly 64:1 lines-to-exports.

The question is whether the *internal* file split adds navigational overhead without adding conceptual separation. At 321 lines total, a developer must hold knowledge of all three files to understand the package. The files do not represent independent abstractions -- they are facets of one concern (HTTP serving).

## 3. Option A: Consolidate to Fewer Files

Merge `handlers.go` and `middleware.go` into `server.go`, producing a single ~321-line file.

**Resulting public API**: Unchanged. Same `New()`, `Start()`, `Shutdown()`, `Handler()`, `RequestID()`.

**Pros**:
- Single file to read for the entire HTTP layer -- no jumping between files to trace a request
- 321 lines is well within Go community norms for a single file (the stdlib `net/http/server.go` is 3,500+ lines)
- Eliminates the mental overhead of "where does this live?"

**Cons**:
- Larger single file (though 321 lines is modest)
- Loses the visual grouping that file names provide (handlers vs middleware)

**Verdict**: Viable. The package is small enough that consolidation improves rather than hinders readability.

## 4. Option B: Functional Options Pattern

Keep the file split but deepen the module with options:

```go
func New(port int, registry Registry, logger *slog.Logger, opts ...Option) *Server
type Option func(*config)
func WithRoutePrefix(prefix string) Option
func WithMiddleware(mw func(http.Handler) http.Handler) Option
func WithTimeouts(read, write, idle time.Duration) Option
```

**Pros**:
- Future extensibility without breaking the constructor signature
- Aligns with widely recognized Go patterns (gRPC, zap, etc.)

**Cons**:
- Premature abstraction: warpgate has one call site for `New()` (in `cmd/warpgate/main.go`). Functional options shine when there are multiple callers with varying needs.
- Adds configuration machinery (`config` struct, option applicators) that must be maintained and tested
- The middleware stack is security-relevant (recovery, security headers) -- making it pluggable invites misconfiguration

**Verdict**: Not recommended. The complexity cost exceeds the benefit for a project with a single constructor call site. If warpgate grows to support multiple server configurations (e.g., admin API vs public API), revisit then.

## 5. Option C: Keep As-Is

The current 3-file split is a reasonable Go convention and does not create measurable friction.

**Pros**:
- Conventional Go project layout -- reviewers expect `handlers.go` and `middleware.go` in a server package
- File-level grouping serves as lightweight documentation of concerns
- Git blame is more granular per-concern

**Cons**:
- At 321 total lines, the files are thin (~80-127 lines each) -- the boundaries add navigation cost without hiding meaningful complexity
- The conceptual coupling is tight: middleware refers to handler types (`statusWriter`), handlers use middleware's `RequestID()`, and `New()` composes both

**Verdict**: Acceptable. Not optimal, but the cost of the split is low.

## 6. Registry Interface Question

### Option 6a: Keep the local `server.Registry` interface (current approach)

```go
// server.go
type Registry interface {
    ListAll(ctx context.Context, filter provider.ResourceFilter) provider.ListResult
    GetResource(ctx context.Context, id string) (*provider.Resource, error)
    CheckHealth(ctx context.Context) []provider.HealthStatus
    Providers() []string
}
```

**Pros**:
- **Dependency inversion**: The server package declares what it needs, not what the provider package offers. This is textbook Go interface design (accept interfaces, return structs).
- **Testability**: Tests can supply a minimal mock that only implements what the server uses, without pulling in `provider.Registry`'s internal state (mutex, timeout, snapshot logic).
- **Decoupling**: If `provider.Registry` gains methods the server doesn't need (e.g., `Register`, `Provider`), the server's contract doesn't expand.

**Cons**:
- The 4 methods and their signatures are a 1:1 copy of the provider's public surface -- today. If they drift, someone must update both.
- The interface still imports `provider.ResourceFilter`, `provider.ListResult`, `provider.HealthStatus`, `provider.Resource`, and `provider.ErrNotFound`. The package-level decoupling is shallow: the server depends on provider's types even though it doesn't depend on its concrete `*Registry`.

### Option 6b: Import `provider.Registry` directly

```go
// server.go
func New(port int, registry *provider.Registry, logger *slog.Logger) *Server
```

**Pros**:
- One source of truth -- no duplicate interface
- Fewer lines in server.go

**Cons**:
- Tests must construct a full `*provider.Registry` with timeout, mutex, etc. (The test already does this today via `provider.NewRegistry`, so the practical cost is low, but it prevents using a lightweight mock.)
- Violates Go's interface segregation norm: the server would accept 7+ methods when it only calls 4

### Recommendation

**Keep the local interface (Option 6a).** The dependency inversion is idiomatic Go and demonstrates understanding of interface segregation -- a signal Blizzard reviewers will recognize. The shallow coupling on provider types is acceptable because those types represent the domain model, not implementation details.

However, consider adding a one-line comment connecting the two:

```go
// Registry defines the subset of provider.Registry the server requires.
type Registry interface { ... }
```

This makes the relationship explicit without coupling the code.

## 7. Test Impact

| Option | Test impact |
|--------|-------------|
| **A (Consolidate)** | Zero. Tests are in `server_test` (external test package) and only use the public API: `server.New()`, `srv.Handler()`. File-internal reorganization is invisible to tests. |
| **B (Functional Options)** | Moderate. Tests that construct via `server.New(port, reg, logger)` still work (options are variadic and optional). New tests needed for each option. |
| **C (Keep As-Is)** | Zero. No changes. |
| **6a (Local interface)** | Zero. Already in place. |
| **6b (Direct import)** | Low. `newTestServer` already constructs a `*provider.Registry`. The mock package would need updating only if tests currently supply a custom mock (they don't -- they use real providers from `mock.DefaultProviders()`). |

## 8. Recommendation

**Option C (Keep As-Is) + the Registry interface comment from 6a.**

Rationale:

1. **Portfolio signal**: A Blizzard Cloud SWE reviewer scanning the repo will recognize the `handlers.go` / `middleware.go` / `server.go` split as standard Go project layout. Consolidating into one file is valid but risks looking like the author doesn't know the convention. For a portfolio project, matching expectations matters more than saving a few file-switch keystrokes.

2. **The split is not harmful**: At 321 lines total, the navigation overhead is minimal. The files are small enough to read in one screen each. The coupling between them (middleware <-> handler types) is normal intra-package coupling, not a design smell.

3. **The local Registry interface is correct**: It demonstrates dependency inversion, interface segregation, and testability awareness. Add the clarifying comment to make the intent explicit.

4. **Functional options are premature**: One call site does not justify the pattern. If warpgate adds an admin server or gRPC gateway, that is the right time to introduce options.

### Concrete action items (for a follow-up implementation bead, not this spike):

- [ ] Add comment to `server.Registry`: `// Registry defines the subset of provider.Registry the server requires.`
- [ ] No file merges or splits needed
- [ ] No functional options
- [ ] Consider extracting `statusWriter` into its own file only if middleware.go grows past ~200 lines (it is currently 114)
