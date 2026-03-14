# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A full-stack OpenTelemetry distributed tracing demo showing W3C Trace Context (traceparent/tracestate) propagation across HTTP, NATS JetStream/Core, WebSocket, and MongoDB change streams.

## Common Commands

### Run the full stack
```bash
make up          # Start all services (auto-detects docker/podman)
make down        # Stop all services
make restart     # Restart all services
make logs        # Tail all logs; SVC=api for a single service
make verify-trace  # Verify end-to-end trace propagation
make up-verify   # Start + verify in one step
```

### Go development (per module)
Each service (`api/`, `worker/`, `dbwatcher/`) and `pkg/otelsetup/` is a separate Go module.

```bash
# Tests
go test ./...           # inside each module directory

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
| worker | 8082 | Consumes NATS (JetStream + Core), serves WebSocket + HTTP `/notify` |
| dbwatcher | — | Watches MongoDB change stream; publishes to NATS `messages.db` |
| frontend | 3000 | React 18 + Grafana Faro; sends messages, receives via WebSocket |
| otel-collector | 4317/4318 | OTLP receiver (gRPC/HTTP); forwards to Tempo |
| tempo | 3200 | Trace storage backend (pinned v2.9.0) |
| grafana | 3001 | Trace visualization |
| nats | 4222 | Message broker with JetStream |
| mongodb | 27017 | Replica set (required for change streams) |

### Message paths (all share one trace)
1. **JetStream**: API → NATS `messages.new` → Worker → WebSocket → Frontend
2. **Core NATS**: API → NATS Core `messages.new` → Worker → WebSocket → Frontend
3. **HTTP Worker**: API → otelresty → Worker `/notify` → WebSocket → Frontend
4. **MongoDB**: API → `otelmongo` insert (stores `_oteltrace` field) → dbwatcher change stream → NATS `messages.db` → Worker → WebSocket → Frontend

### Instrumentation packages (`pkg/instrumentation-go/`)
This is a git submodule at `https://github.com/Marz32onE/instrumentation-go` providing:
- `otel-nats/otelnats` + `oteljetstream` — NATS client instrumentation
- `otel-mongo/otelmongo` — MongoDB client instrumentation (injects `_oteltrace` field)
- `otel-websocket` — WebSocket trace propagation

**Key pattern**: Packages do NOT initialize a TracerProvider. They accept one via options or fall back to `otel.GetTracerProvider()`. Each service calls `otelsetup.Init()` at startup to configure the global provider.

### Shared OTel init (`pkg/otelsetup/`)
`otelsetup.Init(endpoint, attrs...)` creates an OTLP TracerProvider (auto-detects gRPC vs HTTP from endpoint), sets it as the global provider, and sets the W3C propagator. Returns a shutdown function.

## Key Environment Variables

| Variable | Default | Used by |
|----------|---------|---------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | api, worker, dbwatcher |
| `NATS_URL` | `nats://localhost:4222` | api, worker, dbwatcher |
| `MONGODB_URI` | `mongodb://localhost:27017` | api, dbwatcher |
| `WORKER_URL` | `http://worker:8082` | api |
| `VITE_API_URL` | `http://localhost:8088` | frontend (build-time) |
| `VITE_WS_URL` | `ws://localhost:8082` | frontend (build-time) |
| `VITE_OTEL_COLLECTOR_URL` | `http://localhost:4318` | frontend (build-time) |

## Go Module Layout

Each service has its own `go.mod` using **Go 1.26** and **OpenTelemetry v1.42.0**. Local instrumentation packages are referenced via `replace` directives pointing to `../pkg/instrumentation-go/...`.
