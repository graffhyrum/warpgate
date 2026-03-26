# warpgate

Multi-provider infrastructure gateway — a unified HTTP API that queries resource state across cloud providers through a pluggable provider interface.

## Quick Start

```bash
# Build and run
make build
./bin/warpgate

# Or with Go directly
go run ./cmd/warpgate
```

The server starts on `:8080` by default. Configure via environment:

| Variable | Default | Range | Description |
|---|---|---|---|
| `PORT` | `8080` | — | HTTP listen port |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` | Structured log level |
| `PROVIDER_TIMEOUT` | `5s` | 100ms–30s | Per-provider query timeout |

Invalid values are logged as warnings and fall back to defaults.

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Liveness (always 200) |
| GET | `/ready` | Readiness (checks all providers; 503 if none registered) |
| GET | `/api/v1/resources` | List resources (filter: `?provider=&type=&region=&status=`) |
| GET | `/api/v1/resources/{id}` | Get single resource |
| GET | `/api/v1/providers` | List providers + health |

### Examples

```bash
# Liveness check
curl localhost:8080/health
# {"status":"ok"}

# List all resources across providers
curl localhost:8080/api/v1/resources | jq '.count'
# 15

# Filter by provider and type
curl "localhost:8080/api/v1/resources?provider=aws&type=compute" | jq '.resources[].name'
# "web-server-1"
# "web-server-2"
# "batch-worker"

# Get a specific resource
curl localhost:8080/api/v1/resources/aws-ec2-001

# Resource not found → 404 with request ID
curl localhost:8080/api/v1/resources/nonexistent
# {"error":"resource not found","request_id":"...","status":404}

# Check provider health
curl localhost:8080/api/v1/providers
```

### Debugging a Request

Every response includes an `X-Request-Id` header. Correlate it with structured JSON logs:

```bash
$ curl -sI localhost:8080/api/v1/resources/aws-ec2-001 | grep X-Request-Id
X-Request-Id: 3f2a1b4c-8d9e-4f0a-b1c2-d3e4f5a6b7c8
```

Find that request in server logs:
```json
{"time":"...","level":"INFO","msg":"request","request_id":"3f2a1b4c-...","method":"GET","path":"/api/v1/resources/aws-ec2-001","status":200,"duration_ms":1}
```

## Development

```bash
make test    # run tests with race detector
make vet     # go vet
make lint    # golangci-lint (auto-installs if missing)
make build   # build binary to bin/warpgate
```

## Docker

```bash
make docker-build
docker run -p 8080:8080 warpgate
```

## Design

See [ARCHITECTURE.md](ARCHITECTURE.md) for design decisions, the provider interface rationale, and extension guide.
