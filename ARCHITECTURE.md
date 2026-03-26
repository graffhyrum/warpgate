# Architecture

## Overview

warpgate is a multi-provider infrastructure gateway. It provides a single HTTP API that fans out queries across registered cloud providers, collects results, and handles partial failures gracefully.

```
Client → HTTP Server → Registry → [Provider A, Provider B, Provider C]
                                         ↓ (concurrent via errgroup)
                                   Aggregated Response
```

## Provider Interface

The core abstraction is `provider.Provider`:

```go
type Provider interface {
    Name() string
    ListResources(ctx context.Context, filter ResourceFilter) ([]Resource, error)
    GetResource(ctx context.Context, id string) (*Resource, error)
    CheckHealth(ctx context.Context) HealthStatus
}
```

Any backend that satisfies this interface can be registered. The current implementation uses in-process mock providers that return cloud-realistic data for AWS, GCP, and Azure.

## Domain Model

`Resource` is a value object with behavior — `MatchesFilter(f ResourceFilter) bool` moves filter matching into the domain rather than scattering it across each provider implementation.

`ResourceFilter` is constructed via `ParseFilter()`, a factory that validates and bounds input at the system boundary. Raw query strings never directly construct domain types.

## Registry: The Glue Layer

The `Registry` is the architectural centerpiece. It:

1. **Fans out** `ListResources` and `GetResource` calls to all providers concurrently using `errgroup`
2. **Snapshots the provider map** under a read lock, then releases the lock before performing any I/O — preventing slow providers from blocking `Register()` calls
3. **Applies per-provider timeouts** via `context.WithTimeout` to prevent one slow provider from blocking the response
4. **Collects partial results** — if provider A fails, results from B and C are still returned
5. **Distinguishes error types** — `GetResource` returns `ErrNotFound` only when all providers report not-found; real errors (timeouts, auth failures) propagate as wrapped errors
6. **Reports warnings** for failed providers in the response, so the caller knows the result is partial

The server consumes the `Registry` through a local interface (`server.Registry`), not the concrete type — enabling alternative implementations (caching, circuit-breaking) without changing the HTTP layer.

## Adding a New Provider

1. Implement `provider.Provider` in a new package under `internal/`
2. Register it in `cmd/warpgate/main.go`:
   ```go
   registry.Register(myprovider.New(cfg))
   ```
3. The provider automatically appears in all API responses

## Error Handling

- **Sentinel errors**: `provider.ErrNotFound` maps to HTTP 404
- **Error wrapping**: Provider errors are wrapped with `fmt.Errorf("provider %s: %w", name, err)` for `errors.Is` chains
- **Opaque error responses**: Internal provider errors are logged server-side; clients receive `"upstream provider error"` with the request ID for correlation
- **Error response contract**: Every error returns `{"error": "...", "status": N, "request_id": "..."}`
- **Partial failure**: Fan-out queries return available results plus a `warnings` array

## Middleware Stack

Applied outermost-first:

1. **Recovery**: Catches panics, logs stack trace with request ID; only writes 500 if headers haven't been sent (prevents corrupt split responses)
2. **Request ID**: UUID per request via `github.com/google/uuid`, set in context and `X-Request-Id` header
3. **Security headers**: `X-Content-Type-Options: nosniff`
4. **Logging**: Structured JSON via `slog` — method, path, status, duration, request ID

The `statusWriter` wrapper implements `Unwrap() http.ResponseWriter` for `http.ResponseController` compatibility.

## HTTP Server Hardening

- **Timeouts**: `ReadHeaderTimeout` (5s), `ReadTimeout` (10s), `WriteTimeout` (30s), `IdleTimeout` (120s) prevent slowloris and resource exhaustion
- **Readiness**: `/ready` returns 503 when zero providers are registered (vacuous truth guard)
- **Config validation**: Invalid environment variables log a warning and fall back to defaults; `PROVIDER_TIMEOUT` is clamped to [100ms, 30s]

## Production Notes

In production, providers would be separate services behind a service mesh. This demo collapses them into in-process calls to focus on the aggregation pattern. The interface boundary is identical — swapping mock providers for real HTTP clients requires zero changes to the registry or HTTP layer.

Key additions for production:
- Real providers via cloud SDKs (aws-sdk-go-v2, google-cloud-go, azure-sdk-for-go)
- Authentication middleware and per-endpoint authorization
- Rate limiting (token bucket per client IP)
- Pagination on resource list endpoints
- Circuit breakers per provider to handle sustained failures
- Metrics (Prometheus) on per-provider latency and error rates
- Image digest pinning in Dockerfile for supply-chain integrity

## Dependencies

| Package | Purpose |
|---|---|
| `golang.org/x/sync/errgroup` | Bounded concurrent fan-out with error propagation |
| `github.com/google/uuid` | Request ID generation |

Both are minimal, well-maintained, and standard in production Go services.
