# SPIKE: Application Bootstrap Testability (cmd/warpgate)

**Bead**: warpgate-nys
**Timebox**: 2 hours
**Date**: 2026-03-26

## Current State

`cmd/warpgate/main.go` has a two-function structure:

```
main() -> os.Exit(run())
run() int
```

`run()` performs the full bootstrap sequence:

1. `config.Load()` — reads env vars, returns `Config`
2. Constructs a `slog.Logger` from config
3. Creates a `provider.Registry`, registers mock providers
4. `server.New(port, registry, logger)` — builds the HTTP server
5. `signal.NotifyContext` — installs SIGINT/SIGTERM handler
6. Starts the server in a goroutine, blocks on signal or error
7. Graceful shutdown with 10-second timeout

**What is testable today**: Handler-level tests use `httptest.NewServer(srv.Handler())` to skip the listen/shutdown path entirely (see `internal/server/handlers_test.go`). This covers routing and response correctness.

**What is NOT testable today**: The bootstrap path itself — config wiring, provider registration, graceful shutdown behavior, and the error/exit-code contract. Because `run()` owns signal handling and calls `config.Load()` (reads real env), there is no seam to inject test configuration or a cancellable context.

## Option A — `Run(ctx, cfg) error`

Extract the body of `run()` into a public function with injected dependencies:

```go
// cmd/warpgate/app.go
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
    registry := provider.NewRegistry(cfg.ProviderTimeout)
    for _, p := range mock.DefaultProviders() {
        registry.Register(p)
        logger.Info("registered provider", "name", p.Name())
    }

    srv := server.New(cfg.Port, registry, logger)

    serverErr := make(chan error, 1)
    go func() { serverErr <- srv.Start() }()

    logger.Info("server ready", "port", cfg.Port)

    select {
    case err := <-serverErr:
        if err != nil && !errors.Is(err, http.ErrServerClosed) {
            return fmt.Errorf("server error: %w", err)
        }
    case <-ctx.Done():
    }

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    if err := srv.Shutdown(shutdownCtx); err != nil {
        return fmt.Errorf("shutdown error: %w", err)
    }

    if err := <-serverErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
        return fmt.Errorf("server error after shutdown: %w", err)
    }
    logger.Info("server stopped")
    return nil
}
```

`main()` becomes:

```go
func main() {
    cfg := config.Load()
    logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
    slog.SetDefault(logger)

    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
    defer stop()

    if err := Run(ctx, cfg, logger); err != nil {
        logger.Error("fatal", "error", err)
        os.Exit(1)
    }
}
```

**Pros**: Minimal change (~15 lines moved). Clear test seam. Follows the Go standard pattern (e.g., `go test` runner itself uses this shape). Context cancellation replaces signal handling in tests.

**Cons**: `Run` still constructs the registry and server internally. If provider registration ever needs to be tested in isolation, another extraction is needed. Acceptable for current scope.

## Option B — `App` Struct

```go
type App struct {
    cfg      config.Config
    logger   *slog.Logger
    registry *provider.Registry
    server   *server.Server
}

func NewApp(cfg config.Config, logger *slog.Logger) *App { ... }
func (a *App) Run(ctx context.Context) error { ... }
```

**Pros**: Fields are inspectable between `New` and `Run` (useful for complex setup validation). Natural home for methods like `App.HealthCheck()` if the binary gains admin commands.

**Cons**: Adds struct + constructor ceremony for a single entry point. The fields are only consumed by `Run` — no other method uses them. Over-engineered for a binary with one command. Would make sense if warpgate grew subcommands (e.g., `warpgate serve`, `warpgate migrate`), but that is speculative.

## Option C — Functional Composition

```go
func buildServer(cfg config.Config, logger *slog.Logger) *server.Server { ... }
func runWithGracefulShutdown(ctx context.Context, srv *server.Server, logger *slog.Logger) error { ... }
```

**Pros**: Each function is independently testable. `buildServer` can be verified without starting a listener. `runWithGracefulShutdown` can be tested with a pre-built server and test context.

**Cons**: Two functions that are always called in sequence, with no realistic scenario where you'd call one without the other. The decomposition is mechanical rather than driven by a real need for independent variation. Adds cognitive overhead without a proportional benefit.

## Test Examples (Option A)

### Server starts and responds to /health

```go
func TestRun_HealthEndpoint(t *testing.T) {
    t.Parallel()

    // Use port 0 so the OS assigns an ephemeral port.
    cfg := config.Config{
        Port:            0,
        LogLevel:        slog.LevelError,
        ProviderTimeout: 5 * time.Second,
    }
    logger := slog.New(slog.NewTextHandler(io.Discard, nil))

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    errCh := make(chan error, 1)
    go func() { errCh <- Run(ctx, cfg, logger) }()

    // Wait for server to be ready (poll /health).
    var baseURL string
    require.Eventually(t, func() bool {
        // Discover the port from the listener (needs a small API addition).
        resp, err := http.Get(baseURL + "/health")
        if err != nil { return false }
        resp.Body.Close()
        return resp.StatusCode == http.StatusOK
    }, 3*time.Second, 50*time.Millisecond)

    cancel()
    require.NoError(t, <-errCh)
}
```

**Note**: This test reveals a design issue — `Run` does not expose the actual listening address. When using port 0, the test needs the assigned port. This motivates a small API addition: `Run` should accept a callback or `Run` should return the address via a channel. A clean solution:

```go
// RunOpts allows callers to observe internal state for testing.
type RunOpts struct {
    // OnListening is called with the actual listen address once the server is bound.
    OnListening func(addr string)
}

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger, opts *RunOpts) error {
    // ... after srv.Start() succeeds and before blocking:
    if opts != nil && opts.OnListening != nil {
        opts.OnListening(listener.Addr().String())
    }
    // ...
}
```

In tests: pass an `OnListening` callback that writes to a channel; in production: pass `nil`.

### Graceful shutdown completes within timeout

```go
func TestRun_GracefulShutdown(t *testing.T) {
    t.Parallel()

    cfg := config.Config{Port: 0, LogLevel: slog.LevelError, ProviderTimeout: 5 * time.Second}
    logger := slog.New(slog.NewTextHandler(io.Discard, nil))

    ctx, cancel := context.WithCancel(context.Background())

    errCh := make(chan error, 1)
    go func() { errCh <- Run(ctx, cfg, logger) }()

    // Give server time to start.
    time.Sleep(200 * time.Millisecond)

    // Cancel context (simulates SIGINT).
    cancel()

    // Shutdown should complete quickly.
    select {
    case err := <-errCh:
        require.NoError(t, err)
    case <-time.After(5 * time.Second):
        t.Fatal("shutdown did not complete within 5 seconds")
    }
}
```

### Invalid config produces a clear error

```go
func TestRun_InvalidPort(t *testing.T) {
    t.Parallel()

    // Port -1 will fail to bind.
    cfg := config.Config{Port: -1, LogLevel: slog.LevelError, ProviderTimeout: 5 * time.Second}
    logger := slog.New(slog.NewTextHandler(io.Discard, nil))

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    err := Run(ctx, cfg, logger)
    require.Error(t, err)
    require.Contains(t, err.Error(), "server error")
}
```

## What Blizzard Cares About

This pattern demonstrates several production-readiness signals:

1. **Testable bootstrap path**: Production Go services at scale (Kubernetes controller-runtime, Google's `go/service` internal framework, HashiCorp tools) all extract their bootstrap into a testable `Run` function. The pattern appears in the [Kubernetes controller-runtime `Manager.Start(ctx)`](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/manager#Manager.Start), in [HashiCorp Consul's `agent.NewBaseDeps` + `agent.New`](https://github.com/hashicorp/consul), and in Google's internal service framework (not public, but follows the same shape). An untestable `main()` is a red flag in cloud service code review.

2. **Context-driven lifecycle**: Using `context.Context` for shutdown (rather than global signal handlers) is the standard Go cloud pattern. It composes with Kubernetes pod termination (SIGTERM -> context cancel), health check probes, and graceful connection draining. Blizzard's Battle.net services run on Kubernetes; this is directly relevant.

3. **Error propagation over exit codes**: Returning `error` from `Run` instead of calling `os.Exit` directly allows the caller to log, report metrics, or clean up. The `int` return from the current `run()` loses error context.

4. **Dependency injection without a framework**: The `Run(ctx, cfg, logger)` signature injects dependencies explicitly rather than using `fx`, `wire`, or `dig`. For a focused service like warpgate, explicit injection is clearer and avoids the magic that DI frameworks introduce. This is the approach recommended by [Go best practices](https://go.dev/wiki/CodeReviewComments) and used in most Kubernetes ecosystem tools.

5. **Port 0 testing**: Using ephemeral ports for integration tests avoids port conflicts in CI, which is a common source of flaky tests in cloud service pipelines.

## Recommendation

**Option A — `Run(ctx, cfg, logger) error`** with the `RunOpts` callback pattern for port discovery.

Rationale:

- **Right-sized for the codebase**: warpgate is a single-command binary with ~70 lines of bootstrap. An `App` struct (Option B) adds indirection without a consumer. Functional composition (Option C) splits a naturally sequential flow for no practical benefit.

- **Follows established Go conventions**: The `Run(ctx, cfg) error` pattern is the most common in the Go ecosystem for testable CLI entry points. It appears in the standard library's test runner, in `cobra.Command.RunE`, and in production services at Google, HashiCorp, and the Kubernetes project.

- **Minimal diff**: Moving the body of `run()` into `Run()` and adjusting `main()` is approximately a 20-line change. No new packages, no new types (beyond the optional `RunOpts`).

- **Unlocks integration testing**: With this change, `cmd/warpgate/main_test.go` can test the full bootstrap path including config wiring, provider registration, health endpoint, and graceful shutdown — all without process-level testing or signal manipulation.

- **Natural upgrade path**: If warpgate later gains subcommands, `Run` can be promoted to a method on an `App` struct at that point. The function signature is forward-compatible: `App.Run(ctx)` simply closes over `cfg` and `logger` via struct fields instead of parameters.

### Implementation sketch (for the follow-up bead)

1. Add `Addr() string` to `server.Server` (returns the bound address after `Start()`)
2. Extract `Run(ctx, cfg, logger, opts) error` into `cmd/warpgate/app.go`
3. Slim `main()` to config load + signal context + `Run` call
4. Add `cmd/warpgate/app_test.go` with the three test cases above
5. Verify `make lint && go test ./...` passes
