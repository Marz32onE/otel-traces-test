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
make clean         # Stop and remove containers + volumes (full clean)
make restart       # Restart all services
make logs          # Tail all logs; SVC=api for a single service
make ps            # Show service status
make verify-trace  # Verify end-to-end trace propagation (API → Mongo → Tempo)
make up-verify     # Start + verify in one step
make detect        # Show auto-detected COMPOSE_CMD, DOCKER_CMD, KIND_CLUSTER
./scripts/verify-full-path.sh  # Full end-to-end check including all API paths
```

Makefile auto-detects `docker compose` / `podman-compose` / `podman compose`. Override: `COMPOSE_CMD='docker compose' make up`.

### Go development (per module)
Each service (`api/`, `worker/`, `dbwatcher/`) and `pkg/otelsetup/` is a separate Go module.

```bash
# Tests and vet (run inside each module directory)
go vet ./...
go test ./...

# Lint — requires golangci-lint v2 (.golangci.yml uses version: "2" syntax)
# From repo root (all modules):
golangci-lint run ./api/... ./worker/... ./dbwatcher/... ./pkg/...
# Per module:
cd api && golangci-lint run ./...
```

**MANDATORY: After ANY `.go` file change in `api/`, `worker/`, or `dbwatcher/`, run before marking work complete:**
```bash
# In the changed module directory:
go build -o /dev/null .
go test ./...
golangci-lint run ./...
```

### Instrumentation packages (`pkg/instrumentation-go/`)
Git submodule with independent Go modules (`otel-mongo/`, `otel-mongo/v2/`, `otel-nats/`, `otel-gorilla-ws/`).
Each module has its own `go.mod` — lint and test must run **inside each module directory**.

**MANDATORY: After ANY code change to `pkg/instrumentation-go/`, run these checks before considering the work complete:**
```bash
# Per module (run in the module directory that was changed):
cd pkg/instrumentation-go/otel-mongo && golangci-lint run ./...
cd pkg/instrumentation-go/otel-mongo/v2 && golangci-lint run ./...
cd pkg/instrumentation-go/otel-nats && golangci-lint run ./...
cd pkg/instrumentation-go/otel-gorilla-ws && golangci-lint run ./...
```
All modules must report **0 issues**. Common failures: `goimports` (stdlib imports must be in a separate group before third-party), `errcheck`, `govet`.

### Kubernetes / Kind deployment
```bash
make kind-up        # Build images + helm install into Kind cluster
make kind-down      # Helm uninstall
make kind-verify    # Wait for pods + curl API via port-forward
make kind-build     # Build images only and load into Kind
make kind-install   # Helm upgrade --install only
```

Helm chart is at `charts/otel-traces-test/`. Kind cluster is auto-detected (override: `KIND_CLUSTER=mycluster`).

### Frontend development
```bash
cd frontend
npm install
npm run dev      # Vite dev server
npm run build    # Production build
```

### Instrumentation packages (`pkg/instrumentation-js/`)
Git submodule at `https://github.com/Marz32onE/instrumentation-js`. Contains `packages/otel-rxjs-ws` (RxJS wrapper) and `packages/otel-ws` (native Node ws). Wire formats match Go `otel-gorilla-ws` (embedded send; embedded + header envelope receive).

```bash
cd pkg/instrumentation-js
make install       # Install dependencies (all packages)
make build         # Build all packages (required before frontend can use it)
make test          # Run all tests (Jest)
make lint          # TypeScript type-check
make publish-dry   # Dry-run npm pack to verify publish contents
make clean         # Remove build artefacts
```

**MANDATORY: After ANY code change to `pkg/instrumentation-js/`, rebuild before testing in frontend:**
```bash
cd pkg/instrumentation-js && make build
# Then rebuild frontend:
cd frontend && npm install && npm run build
```

Frontend references this package via a local `file:` path (no npm publish needed for local dev):
```json
"@marz32one/otel-rxjs-ws": "file:../pkg/instrumentation-js/packages/otel-rxjs-ws"
```

The published layout matches npm conventions: **`type: "module"`**, **`exports`** (with `types` + `import`), and Vite resolves **`dist/index.js`** after `make build` — no special Vite alias required.

### Minimal WebSocket trace stack
A standalone demo using `ws-node-backend` (Node.js/TypeScript) + `frontend-ws-trace` + `docker-compose.ws-trace.yml`. Runs without Go services — uses `pkg/instrumentation-js/otel-ws` for trace propagation.

```bash
make up-ws-trace       # Build + start (tempo, grafana, otel-collector, ws-node-backend :8085, frontend-ws-trace :3000)
make down-ws-trace     # Stop
make logs-ws-trace     # Tail logs; SVC=ws-node-backend for a single service
make verify-ws-trace   # Verify WebSocket trace propagation via Tempo
```

`ws-node-backend` env: `PORT=8085`, `OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318`.

### Submodule setup
```bash
git submodule update --init   # First-time or after pulling (initialises both instrumentation-go and instrumentation-js)
```

## Architecture

### Services
| Service | Port | Role | Framework |
|---------|------|------|-----------|
| api | 8088 | HTTP entry point; publishes to NATS, calls Worker, writes to MongoDB | Gin + otelgin |
| api-mongo-v1 | 8089 | Legacy version of api (MongoDB path only) | Gin |
| worker | 8082 | Consumes NATS (JetStream + Core), serves WebSocket (`GET /ws`) + HTTP (`POST /notify`) | net/http + gorilla/websocket + otelhttp |
| dbwatcher | — | Watches MongoDB `messaging.messages` change stream (all CRUD); publishes to NATS `messages.db` | daemon (no HTTP) |
| frontend | 3000 | React 18 + Grafana Faro; sends messages, receives via WebSocket | Vite + React 18 + @grafana/faro |
| otel-collector | 4317/4318 | OTLP receiver (gRPC/HTTP with CORS for browser); forwards to Tempo | — |
| tempo | 3200 | Trace storage backend (**pinned v2.9.0** — v2.10+ is incompatible) | — |
| grafana | 3001 | Trace visualization (anonymous Admin; Tempo datasource pre-configured) | — |
| nats | 4222 | Message broker with JetStream | — |
| mongodb | 27017 | Replica set (required for change streams) | — |

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

### Instrumentation packages

#### Go (`pkg/instrumentation-go/`)
Git submodule at `https://github.com/Marz32onE/instrumentation-go` (branch `feat/trace-propagation-mod`):
- `otel-nats/otelnats` + `oteljetstream` — NATS client instrumentation
- `otel-mongo/otelmongo` — MongoDB client instrumentation (injects `_oteltrace` field)
- `otel-gorilla-ws` — WebSocket trace propagation for gorilla/websocket

**Key pattern**: Packages do NOT initialize a TracerProvider. They accept one via options or fall back to `otel.GetTracerProvider()`. Each service calls `otelsetup.Init()` at startup to configure the global provider, then `defer otelsetup.Shutdown(tp)`.

Each package has an `example/` directory showing the full init pattern.

#### JavaScript (`pkg/instrumentation-js/`)
Git submodule at `https://github.com/Marz32onE/instrumentation-js`:
- `packages/otel-rxjs-ws` — **Same public API as `rxjs/webSocket`** (`webSocket`, `WebSocketSubject`, `WebSocketSubjectConfig` only). Trace propagation uses global `propagation` / `trace.getTracerProvider()`.
- `packages/otel-ws` — Native Node `ws` wrapper with trace propagation and send/receive spans.

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

## Configuration Files

- OTel Collector config: `charts/otel-traces-test/config/otel-collector.yaml` (shared by docker-compose and Helm)
- Tempo config: `charts/otel-traces-test/config/tempo.yaml`
- Grafana datasource: `grafana/provisioning/datasources/tempo.yml`
- golangci-lint: `.golangci.yml` (v2 syntax, linters: errcheck, govet, ineffassign, staticcheck, unused)

## Docker Image Constraints

| Image | Pin | Reason |
|-------|-----|--------|
| `grafana/tempo` | `2.9.0` (NOT `latest`/`2.10+`) | v2.10+ requires partition ring + memcached; simple local-storage config incompatible |
| `nats` | `nats:alpine` (NOT `nats:latest`) | `nats:latest` lacks `wget`; healthcheck in docker-compose.yml depends on it |

## E2E Trace Verification

```bash
TRACE_ID="deadbeef000000000000000000000001"
SPAN_ID="1234567890abcdef"
curl -s http://localhost:8088/api/message -X POST \
  -H "Content-Type: application/json" \
  -H "traceparent: 00-${TRACE_ID}-${SPAN_ID}-01" \
  -d '{"text":"verify-test"}'
# Expect: {"status":"published"}

sleep 5
curl -s "http://localhost:3200/api/traces/${TRACE_ID}" | head -c 500
# Expect: JSON with api spans including producer span and POST /api/message
```

Run this after changes to `api/main.go`, `frontend/src/tracing.ts`, `otel-collector-config.yaml`, `tempo.yaml`, or `docker-compose.yml`.

## Troubleshooting

- **Traces missing after Collector restart**: `docker compose restart otel-collector api worker dbwatcher`
- **Worker spans missing in Tempo / export errors like `otel-collector/otel-collector/v1/traces`**: The OTLP HTTP exporter applies env first, then options. `WithEndpoint(host:port)` only sets the host; a bad `OTEL_EXPORTER_OTLP_ENDPOINT` like `otel-collector` can leave `URLPath` wrong. Use **`WithEndpointURL`** with a full `http://host:port` (as `otelsetup` does via `otlpHTTPExporterURL`) so Host and Path are consistent. In Docker, bare `otel-collector` defaults to OTLP/HTTP **4318**; use `otel-collector:4317` for gRPC.
- **Frontend changes not applied**: `docker compose build --no-cache frontend && docker compose up -d frontend`
- **Tempo returns 404 for trace**: Wait a few seconds after sending, then query again
- **Browser spans missing / `rootServiceName: <root span not yet received>`**: Check if the frontend OTLP export is reaching the Collector. The official `@opentelemetry/exporter-trace-otlp-http` may silently fail in browser (uses `sendBeacon`/`XMLHttpRequest`); the project uses a custom `fetch(keepalive:true)` exporter in `frontend/src/tracing.ts` instead. Verify with: `curl -s "http://localhost:3200/api/search?q={}&limit=5"` — look for `rootServiceName: frontend`.
- **Duplicate messages displayed**: Each API endpoint + NATS subject combination must ensure the message is only broadcast once. Check that `api` and `worker` subject names are consistent and no double-consume path exists.
