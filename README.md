# warpgate

[![CI](https://github.com/graffhyrum/warpgate/actions/workflows/ci.yaml/badge.svg)](https://github.com/graffhyrum/warpgate/actions/workflows/ci.yaml)

A multi-provider infrastructure gateway that aggregates resource state across cloud providers through a pluggable provider interface. Built as a focused demonstration of Go patterns used in production cloud infrastructure services.

## What This Demonstrates

| Skill Area | Implementation |
|---|---|
| **Go idioms** | Interfaces, typed string constants, `errgroup` fan-out, `context` propagation, `slog` structured logging, error wrapping with `%w`, table-driven tests |
| **Cloud provider aggregation** | `Provider` interface with 3 mock implementations (AWS, GCP, Azure) returning region-appropriate, cloud-realistic resource data |
| **Concurrent distributed patterns** | Registry fans out to providers concurrently via `errgroup` with per-provider `context.WithTimeout`; partial failures collected as warnings alongside available results; `GetResource` distinguishes not-found from real provider errors across concurrent lookups |
| **HTTP server hardening** | Read/write/idle timeouts, panic recovery with partial-write detection, opaque error responses (internal details logged server-side, correlated via `X-Request-Id`), input validation at the system boundary |
| **Debugging across the stack** | Every request gets a UUID in the `X-Request-Id` response header, threaded through `slog` context for end-to-end correlation between client response and server logs |
| **CI/CD + containers** | GitHub Actions (vet, test -race, golangci-lint, CGO_ENABLED=0 build), multi-stage Dockerfile with `go mod verify` and distroless nonroot runtime |
| **Terminal tooling** | [Bubbletea](https://github.com/charmbracelet/bubbletea) TUI dashboard with live provider health monitoring and resource inventory |

## Project Structure

```
warpgate/
├── cmd/
│   ├── warpgate/          # HTTP server entry point
│   └── warpgate-tui/      # Terminal dashboard (Bubbletea)
├── internal/
│   ├── provider/          # Core interface, types, registry (the "glue" layer)
│   ├── mock/              # AWS/GCP/Azure mock providers
│   ├── server/            # HTTP handlers, middleware, routing
│   └── config/            # Environment-based configuration
├── Dockerfile             # Multi-stage: golang:1.26-alpine → distroless
├── .github/workflows/     # CI pipeline
├── .golangci.yml          # Curated linter configuration
└── ARCHITECTURE.md        # Design decisions and extension guide
```

## Quick Start

```bash
make build
./bin/warpgate            # start the server on :8080
./bin/warpgate-tui        # launch the TUI dashboard (separate terminal)
```

Configure via environment:

| Variable | Default | Range | Description |
|---|---|---|---|
| `PORT` | `8080` | -- | HTTP listen port |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` | Structured log level |
| `PROVIDER_TIMEOUT` | `5s` | 100ms--30s | Per-provider query timeout (clamped; invalid values logged as warnings) |

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Liveness probe (always 200) |
| GET | `/ready` | Readiness probe (503 if no providers healthy) |
| GET | `/api/v1/resources` | List resources (`?provider=&type=&region=&status=`) |
| GET | `/api/v1/resources/{id}` | Get single resource (404 / 502 with request ID) |
| GET | `/api/v1/providers` | Provider health + latency |

```bash
curl localhost:8080/api/v1/resources | jq '.count'
# 15

curl "localhost:8080/api/v1/resources?provider=aws&type=compute" | jq '.resources[].name'
# "web-server-1"
# "web-server-2"
# "batch-worker"

curl localhost:8080/api/v1/resources/nonexistent
# {"error":"resource not found","request_id":"...","status":404}
```

### Request Tracing

Every response includes `X-Request-Id`. Correlate with structured JSON logs:

```bash
curl -sI localhost:8080/health | grep X-Request-Id
# X-Request-Id: 3f2a1b4c-8d9e-4f0a-b1c2-d3e4f5a6b7c8
```
```json
{"time":"...","level":"INFO","msg":"request","request_id":"3f2a1b4c-...","method":"GET","path":"/health","status":200,"duration_ms":0}
```

## TUI Dashboard

A terminal dashboard built with [Bubbletea](https://github.com/charmbracelet/bubbletea) / [Lipgloss](https://github.com/charmbracelet/lipgloss) / [Bubbles](https://github.com/charmbracelet/bubbles) that displays live provider health and resource inventory with auto-refresh.

The TUI connects to a running warpgate server -- start the server first:

```bash
./bin/warpgate &                            # start the server (terminal 1)
./bin/warpgate-tui                          # launch the dashboard (terminal 2)
./bin/warpgate-tui http://remote-host:8080  # or connect to a remote server
```

If the server isn't running, the TUI shows a connection error and retries automatically every 3 seconds.

`↑`/`↓` navigate resources, `r` manual refresh, `q` quit.

## Development

```bash
make test     # go test -race ./...
make vet      # go vet ./...
make lint     # golangci-lint with curated ruleset
make build    # build both server and TUI to bin/
```

## Docker

```bash
make docker-build
docker run -p 8080:8080 warpgate
```

## Design

See [ARCHITECTURE.md](ARCHITECTURE.md) for design decisions: provider interface rationale, registry lock/snapshot strategy, middleware ordering, server hardening, and production extension notes.
