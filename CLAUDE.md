# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

繁體中文說明：[README.zh-TW.md](README.zh-TW.md)

## Project Overview

A full-stack OpenTelemetry distributed tracing demo showing W3C Trace Context (traceparent/tracestate) propagation across HTTP, NATS JetStream/Core, WebSocket, and MongoDB change streams. One trace runs from browser send to browser receive across all services, viewable in Grafana/Tempo.

## Common Commands

### Run the full stack
```bash
make up            # Start all services (auto-detects docker/podman)
make down          # Stop all services
make restart       # Restart all services
make logs          # Tail all logs; SVC=api for a single service
make verify-trace  # Verify end-to-end trace propagation (API → Mongo → Tempo)
make up-verify     # Start + verify in one step
./scripts/verify-full-path.sh  # Full end-to-end check including all API paths
```

Makefile defaults to `podman compose`. Override: `COMPOSE_CMD='docker compose' make up`.

### Go development (per module)
Each service (`api/`, `worker/`, `dbwatcher/`) and `pkg/otelsetup/` is a separate Go module.

```bash
# Tests and vet (run inside each module directory)
go vet ./...
go test ./...

# Lint (from repo root)
golangci-lint run ./api/... ./worker/... ./dbwatcher/... ./pkg/...
# or per module:
cd api && golangci-lint run ./...
```

### Frontend development
```bash
cd frontend
npm install
npm run dev      # Vite dev server
npm run build    # Production build
```

### Submodule setup
```bash
git submodule update --init   # First-time or after pulling
```

## Architecture

### Services
| Service | Port | Role |
|---------|------|------|
| api | 8088 | Gin HTTP entry point; publishes to NATS, calls Worker, writes to MongoDB |
| api-mongo-v1 | 8089 | Legacy version of api (MongoDB path only) |
| worker | 8082 | Consumes NATS (JetStream + Core), serves WebSocket (`GET /ws`) + HTTP (`POST /notify`) |
| dbwatcher | — | Watches MongoDB `messaging.messages` change stream (all CRUD); publishes to NATS `messages.db` |
| frontend | 3000 | React 18 + Grafana Faro; sends messages, receives via WebSocket |
| otel-collector | 4317/4318 | OTLP receiver (gRPC/HTTP with CORS for browser); forwards to Tempo |
| tempo | 3200 | Trace storage backend (**pinned v2.9.0** — v2.10+ is incompatible) |
| grafana | 3001 | Trace visualization (anonymous Admin; Tempo datasource pre-configured) |
| nats | 4222 | Message broker with JetStream |
| mongodb | 27017 | Replica set (required for change streams) |

### Why OTel Collector?
Go services send traces via gRPC directly. The browser must use HTTP OTLP, but Tempo's OTLP receiver does not support CORS. The Collector's HTTP receiver bridges browser → Tempo with CORS support.

### Message paths (all share one trace)
1. **JetStream**: `POST /api/message` → NATS `messages.new` → Worker → WebSocket → Frontend
2. **Core NATS**: `POST /api/message-core` → NATS `messages.core` → Worker → WebSocket → Frontend
3. **HTTP Worker**: `POST /api/message-via-worker` → otelresty → Worker `POST /notify` → WebSocket → Frontend
4. **MongoDB**: `POST /api/message-mongo` → `otelmongo` insert (stores `_oteltrace` field in `messaging.messages`) → dbwatcher change stream → NATS `messages.db` → Worker → WebSocket → Frontend

dbwatcher handles all CRUD: on insert/update/replace it publishes document `text` to `messages.db`; on delete it publishes `{"op":"delete","id":"<hex>"}`.

Worker broadcasts `traceparent`, `tracestate`, `body`, and `api` fields over WebSocket JSON. Frontend extracts context via `propagation.extract` and creates a `receive message` CONSUMER span.

### Trace spans per path
```
JetStream:
  Frontend: send-message-jetstream (CLIENT)
    └─ API: POST /api/message (otelgin, SERVER)
         └─ API: messages.new publish (oteljetstream, PRODUCER)
              └─ Worker: messages.new receive (oteljetstream, CONSUMER)
                   └─ Frontend: receive message (CONSUMER)

Worker HTTP:
  Frontend: send-message-via-worker-http (CLIENT)
    └─ API: POST /api/message-via-worker (otelgin, SERVER)
         └─ API: resty POST /notify (otelresty, CLIENT)
              └─ Worker: POST /notify (otelhttp, SERVER)

MongoDB:
  Frontend: send-message-mongo (CLIENT)
    └─ API: POST /api/message-mongo (otelgin, SERVER)
         └─ API: MongoDB insert (otelmongo)
              └─ dbwatcher: change stream → Publish messages.db
                   └─ Worker: messages.db receive (CONSUMER)
                        └─ Frontend: receive message (CONSUMER)
```

### Instrumentation packages (`pkg/instrumentation-go/`)
Git submodule at `https://github.com/Marz32onE/instrumentation-go` (branch `feat/trace-propagation-mod`):
- `otel-nats/otelnats` + `oteljetstream` — NATS client instrumentation
- `otel-mongo/otelmongo` — MongoDB client instrumentation (injects `_oteltrace` field)
- `otel-websocket` — WebSocket trace propagation

**Key pattern**: Packages do NOT initialize a TracerProvider. They accept one via options or fall back to `otel.GetTracerProvider()`. Each service calls `otelsetup.Init()` at startup to configure the global provider, then `defer otelsetup.Shutdown(tp)`.

Each package has an `example/` directory showing the full init pattern.

### Shared OTel init (`pkg/otelsetup/`)
`otelsetup.Init(endpoint, attrs...)` creates an OTLP TracerProvider (auto-detects gRPC vs HTTP from endpoint), sets it as the global provider, and sets the W3C propagator. Returns a shutdown function.

## Key Environment Variables

| Variable | Default | Used by |
|----------|---------|---------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | api, worker, dbwatcher (use `otel-collector:4317` in Docker) |
| `NATS_URL` | `nats://localhost:4222` | api, worker, dbwatcher |
| `MONGODB_URI` | `mongodb://localhost:27017` | api, dbwatcher |
| `WORKER_URL` | `http://worker:8082` | api (for `/api/message-via-worker` via otelresty) |
| `VITE_API_URL` | `http://localhost:8088` | frontend (build-time) |
| `VITE_WS_URL` | `ws://localhost:8082` | frontend (build-time) |
| `VITE_OTEL_COLLECTOR_URL` | `http://localhost:4318` | frontend (build-time) |

## Go Module Layout

Each service has its own `go.mod` using **Go 1.26** and **OpenTelemetry v1.42.0**. Local instrumentation packages are referenced via `replace` directives pointing to `../pkg/instrumentation-go/...`. Dockerfiles use the repo root as build context and copy `pkg/instrumentation-go` into the image.

## Troubleshooting

- **Traces missing after Collector restart**: `docker compose restart otel-collector api worker dbwatcher`
- **Worker spans missing in Tempo**: Ensure `OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317` in `docker-compose.yml`
- **Frontend changes not applied**: `docker compose build --no-cache frontend && docker compose up -d frontend`
- **Tempo returns 404 for trace**: Wait a few seconds after sending, then query again
