# OTel Traces Test

[繁體中文 (Traditional Chinese)](README.zh-TW.md)

---

A full-stack example demonstrating **OpenTelemetry distributed tracing**. Users send messages from the browser over four paths: **JetStream** (API publishes to NATS), **Core NATS** (fire-and-go), **Worker HTTP** (API → Worker via otelresty), and **MongoDB** (API writes to Mongo → dbwatcher publishes **id** to NATS → Worker **reads Mongo** then broadcasts). The Worker subscribes and broadcasts back to the frontend over WebSocket; **one trace runs from frontend send to frontend receive** across all services and can be viewed in Grafana/Tempo.

---

## Summary for first-time users and AI agents

- **Goal:** End-to-end W3C Trace Context propagation: browser → API (HTTP + NATS publish) → Worker (NATS subscribe + WebSocket broadcast) → browser (WebSocket receive and final span).
- **Stack:** React 18 + TypeScript + Vite (frontend; **Grafana Faro SDK** for trace and W3C propagation), Go + Gin (API), Go + net/http + otelhttp (Worker), NATS (JetStream + Core), OpenTelemetry SDK, OTel Collector, Grafana Tempo, Grafana.
- **Four paths:** **JetStream** (`POST /api/message` → `messages.new` → Worker), **Core NATS** (`POST /api/message-core` → `messages.core` → Worker), **Worker HTTP** (`POST /api/message-via-worker` → API calls Worker `POST /notify` via **otelresty** → trace propagates over HTTP), **MongoDB** (`POST /api/message-mongo` → Mongo → **dbwatcher** (id on `messages.db`) → Worker **FindOne** → WebSocket). Query by trace ID in Tempo.
- **Instrumentation:** `pkg/instrumentation-go` (Git submodule) — [instrumentation-go](https://github.com/Marz32onE/instrumentation-go): **otel-nats** (otelnats + oteljetstream), **otel-mongo** (otelmongo), **otel-gorilla-ws**. Packages accept **TracerProvider** and **Propagators** via options; they do **not** provide InitTracer. The app initializes the global provider and propagator at startup (see **pkg/otelsetup** and each package’s **example/**). API uses [github.com/dubonzi/otelresty](https://github.com/dubonzi/otelresty) for go-resty (spans + trace propagation).
- **Build & run:** Docker Compose builds from repo root; `api`, `worker`, and `dbwatcher` use root as build context and copy `pkg/instrumentation-go`. Use `make up` or `docker compose up --build` (Makefile defaults to `podman compose`; override with `COMPOSE_CMD='docker compose'`).
- **Config:** `api`, `worker`, and `dbwatcher` need `OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317` to send spans to Tempo; frontend uses `VITE_OTEL_COLLECTOR_URL` for OTLP/HTTP via Collector (CORS) to Tempo.

---

## Architecture

```
┌──────────┐   HTTP POST    ┌──────────┐   NATS JetStream   ┌──────────┐
│          │  (traceparent) │          │   messages.new     │          │
│ Frontend ├───────────────►│   API    ├───────────────────►│  Worker  │
│ :3000    │                │  :8088   │   messages.db      │  :8082   │
│          │  /message-via- │     │    │  (trace context)  │     ▲    │
│          │  worker (HTTP)─┼─────┼────┼── POST /notify ───┼─────┘    │
│          │  /message-mongo│     ▼    │  (otelresty)       │ (net/http│
│          │                │ ┌────────┐   change stream   │ +otelhttp)│
│          │                │ │MongoDB │──► dbwatcher ──────┼─────┐    │
│          │                │ │ :27017 │   (→ messages.db) │     │    │
│          │◄─────────────────────────────────────────────────────────┤
│          │   WebSocket (JSON: traceparent, tracestate, body)         │
└────┬─────┘                                                └──────────┘
     │  OTLP/HTTP (browser)           OTLP/gRPC (api, worker, dbwatcher)
     ▼                                                      │
┌──────────────┐    OTLP/gRPC (to Tempo)   ◄───────────────┘
│     OTel     │
│  Collector   │──────────────────────────► ┌──────────┐
│ :4317 :4318  │                             │  Tempo   │
└──────────────┘                             │  :3200   │
                                             └────┬─────┘
                                             ┌────▼─────┐
                                             │ Grafana  │
                                             │  :3001   │
                                             └──────────┘
```

### Why OTel Collector?

API and Worker (Go) can send traces to Tempo via gRPC. The **browser** must use HTTP OTLP, and Tempo’s OTLP receiver does not support CORS, so the browser would be blocked. OTel Collector’s HTTP receiver supports CORS and bridges browser and Tempo.

---

## Services

| Service          | Tech                     | Purpose                                                                                                                                 | Port                                   |
|------------------|--------------------------|-----------------------------------------------------------------------------------------------------------------------------------------|----------------------------------------|
| **Frontend**     | React 18 + TS + Vite     | UI: two groups (NATS: JetStream/Core; MongoDB: Insert/Update/Read/Delete with editable id). Shared results area at bottom: messages from WebSocket (Worker monitoring NATS). Trace flow hint per group. | `3000`                                 |
| **API**          | Go (Gin) + otelresty     | Endpoints: `POST /api/message` (JetStream), `POST /api/message-core` (Core), `POST /api/message-via-worker` (HTTP to Worker via otelresty), `POST /api/message-mongo` (MongoDB), `...-mongo-update/read/delete`; returns `trace_id`. Env: `WORKER_URL` (default `http://worker:8082`). | `8088`                                 |
| **Worker**       | Go (net/http + otelhttp) | `GET /ws` (WebSocket), `POST /notify` (HTTP, used by API/otelresty); JetStream + Core Subscribe; broadcasts **traceparent/tracestate + body + api** over WebSocket. | `8082`                                 |
| **MongoDB**      | Mongo 7 (replica set)    | Stores `messaging.messages`; otelmongo injects `_oteltrace` on write. dbwatcher uses change stream (replica set required).             | `27017`                                |
| **dbwatcher**    | Go + mongo-driver        | Watches `messaging.messages` **CRUD**; publishes **document id** (and trace via `_oteltrace`) on `messages.db`. Worker loads text from Mongo then broadcasts. | no public port                         |
| **NATS**         | NATS Alpine + JetStream  | Message queue; healthcheck uses `wget` (alpine)                                                                                         | `4222` (client), `8222` (monitoring)   |
| **OTel Collector** | OTel Collector Contrib | Receives gRPC/HTTP OTLP, forwards to Tempo; HTTP endpoint has CORS for browser                                                          | `4317` (gRPC), `4318` (HTTP)           |
| **Tempo**        | Grafana Tempo 2.9.0      | Trace backend (**pinned 2.9.0**; v2.10+ incompatible)                                                                                    | `3200`                                 |
| **Grafana**      | Grafana latest           | UI; anonymous Admin; Tempo datasource configured                                                                                        | `3001`                                 |
| **ws-node-backend** | Node.js + TypeScript  | Minimal standalone demo using `@marz32one/otel-ws`; serves `/otel-ws` (instrumented) and `/ws` (plain) WebSocket paths; echoes messages with trace ID. Used by `docker-compose.ws-trace.yml`. | `8085` |

---

## Message flow

1. User enters a message in **NATS** or **MongoDB** group and clicks the relevant button (JetStream, Core NATS, or MongoDB Insert/Update/Read/Delete). MongoDB actions use an editable **ID** field (default `_id`; after Insert the returned id is synced).
2. Frontend creates the appropriate send span (CLIENT), propagates via `traceparent`/`tracestate` headers, and sends HTTP POST to API.
3. API’s `otelgin` continues the trace; **JetStream** path uses `oteljetstream.Publish`; **Core** uses `otelnats.Publish`; **MongoDB** path uses otelmongo Collection (Insert/Update/Read/Delete) and returns `trace_id`.
4. **MongoDB path:** dbwatcher publishes **`{"op":"change","id":"<hex>"}`** on insert/update/replace (trace context from document `_oteltrace` in NATS headers) and **`{"op":"delete","id":"<hex>"}`** on delete. Worker consumes `messages.db`, **FindOne** by id for `change`, then broadcasts the document `text` (or delete JSON) over WebSocket so traces chain: API → Mongo → dbwatcher → NATS → Worker → Mongo read → WebSocket.
5. **JetStream/Core:** Worker receives via JetStream (Consume/Messages) or Core Subscribe; **otelnats** / **oteljetstream** extract trace from headers, create receive span, and broadcast over WebSocket.
6. Frontend receives via WebSocket; the **shared results area** at the bottom shows all messages. If `traceparent` and `body` exist, frontend uses `propagation.extract` and creates `receive message` span (CONSUMER). UI shows **Trace verification** and last `trace_id` for Grafana/Tempo.

---

## Trace flow (in Grafana)

**JetStream path** (Core uses `messages.core`):

```
Frontend: send-message-jetstream          (SpanKind CLIENT)
  └─ API: POST /api/message               (otelgin, SERVER)
       └─ API: messages.new publish       (oteljetstream, PRODUCER)
            └─ Worker: messages.new receive (oteljetstream, CONSUMER)
                 └─ Frontend: receive message (CONSUMER)
```

**Worker HTTP path** (otelresty + otelhttp):

```
Frontend: send-message-via-worker-http    (CLIENT)
  └─ API: POST /api/message-via-worker    (otelgin, SERVER)
       └─ API: resty POST /notify          (otelresty, CLIENT)
            └─ Worker: POST /notify         (otelhttp, SERVER)
```

**MongoDB path:**

```
Frontend: send-message-mongo              (CLIENT)
  └─ API: POST /api/message-mongo         (otelgin, SERVER)
       └─ API: MongoDB insert             (otelmongo)
            └─ dbwatcher: change stream → Publish messages.db
                 └─ Worker: messages.db receive (CONSUMER)
                      └─ Frontend: receive message (CONSUMER)
```

- **Frontend:** OTLP/HTTP → OTel Collector (CORS).
- **API/Worker/dbwatcher:** OTLP/gRPC → OTel Collector → Tempo.
- API CORS allows `traceparent`, `tracestate`.

---

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) + [Docker Compose](https://docs.docker.com/compose/) (or Podman Compose; see Makefile)
- [Git](https://git-scm.com/) (with submodule support)

### Development: tests and lint

- Go tests use **testify** in `pkg/instrumentation-go` (otel-nats, otel-mongo) and in `api`/`worker`/`dbwatcher` where applicable.
- Run **`go vet ./...`** and **`go test ./...`** in each module (`api`, `worker`, `dbwatcher`, `pkg/otelsetup`, `pkg/instrumentation-go/otel-nats`, `pkg/instrumentation-go/otel-mongo`, `pkg/instrumentation-go/otel-gorilla-ws`).
- With [golangci-lint](https://golangci-lint.run/) installed, run it per Go module directory.

---

## Quick start

```bash
git clone --recurse-submodules git@github.com:Marz32onE/otel-traces-test.git
cd otel-traces-test

# If already cloned without submodules
git submodule update --init

# Start all services (recommended)
make up
# Docker users:
# COMPOSE_CMD='docker compose' make up
# Or directly:
# docker compose up --build
```

| Service          | URL                     |
|------------------|-------------------------|
| Frontend         | http://localhost:3000   |
| Grafana          | http://localhost:3001   |
| NATS Monitoring  | http://localhost:8222   |
| Tempo API        | http://localhost:3200   |

### View traces

1. Open http://localhost:3001 (Grafana).
2. **Explore** → datasource **Tempo** → **Search** → Service name `frontend`, `api`, or `worker`.
3. Open a trace to see the full path from send to receive.

### Verify trace (CLI)

```bash
RES=$(curl -s -X POST http://localhost:8088/api/message -H "Content-Type: application/json" -d '{"text":"hello"}')
echo "$RES"   # {"trace_id":"..."}
```

### Check whole trace propagation path

After `make up`, run:

```bash
make verify-trace
```

This sends a request with a known trace ID to `POST /api/message-mongo`, waits for OTLP flush, then queries Tempo and verifies the trace is present. Exit 0 means propagation (API → Mongo → Tempo) is working.

One-shot: bring up the stack and run the check:

```bash
make up-verify
```

For a full trace including the frontend, send a message from http://localhost:3000 and use the **Trace verification** / `trace_id` on the page in Grafana Explore → Tempo.

### Verify full path (with MongoDB)

```bash
./scripts/verify-full-path.sh
```

Requires `go` and one compose runtime: `docker compose`, `podman compose`, or `podman-compose`. The script auto-detects compose command, builds, brings up services, hits all API endpoints, and checks dbwatcher/worker logs and Tempo.

Manual MongoDB path check:

```bash
curl -s -X POST http://localhost:8088/api/message-mongo -H "Content-Type: application/json" -d '{"text":"mongo-e2e"}'
make logs SVC=dbwatcher
make logs SVC=worker
```

### Stop and clean

```bash
make down
make clean   # with volumes
# Docker users can also run:
# docker compose down
# docker compose down -v
```

---

## Project structure

```
.
├── api/                    # Go + Gin API (endpoints, trace_id)
├── worker/                 # Go net/http + otelhttp (WebSocket + POST /notify)
├── dbwatcher/              # Mongo change stream → NATS
├── frontend/               # React + Vite + Grafana Faro
├── pkg/
│   ├── instrumentation-go/   # Git submodule — otel-nats, otel-mongo, otel-gorilla-ws
│   │   ├── otel-nats/          # otelnats, oteljetstream (NATS + JetStream OTel)
│   │   ├── otel-mongo/        # otelmongo (MongoDB OTel, _oteltrace)
│   │   ├── otel-gorilla-ws/   # WebSocket trace propagation
│   │   └── example/           # (per package) how to init TracerProvider + use package
│   └── otelsetup/             # Shared OTLP TracerProvider init (Init, Shutdown)
├── charts/otel-traces-test/config/
├── grafana/
├── docker-compose.yml
├── Makefile
└── LICENSE
```

`api`, `worker`, and `dbwatcher` Dockerfiles use the repo root as build context and copy `pkg/instrumentation-go`.

### Tracing: provider/propagator init (per OTel Go Contrib)

Instrumentation packages (**otelnats**, **oteljetstream**, **otelmongo**, **otelgorillaws**) do **not** provide `InitTracer`. They accept **TracerProvider** and **Propagators** via options and default to `otel.GetTracerProvider()` / `otel.GetTextMapPropagator()`.

- **This repo:** `api`, `worker`, and `dbwatcher` use **pkg/otelsetup**: call **`otelsetup.Init("", attribute.String("service.name", "api"), ...)`** once at startup, then **`defer otelsetup.Shutdown(tp)`**. After that, use `otelnats.Connect`, `otelmongo.NewClient`, etc. with no per-package init.
- **Examples:** Each package under `pkg/instrumentation-go` has an **example/** directory showing how to create an OTLP TracerProvider, set `otel.SetTracerProvider` and `otel.SetTextMapPropagator`, then use the package.

---

## Git submodules

| Path                      | Description |
|---------------------------|-------------|
| `pkg/instrumentation-go`  | [instrumentation-go](https://github.com/Marz32onE/instrumentation-go) — otel-nats (otelnats + oteljetstream), otel-mongo (otelmongo), otel-gorilla-ws. Branch: `feat/trace-propagation-mod`. |
| `pkg/instrumentation-js`  | [instrumentation-js](https://github.com/Marz32onE/instrumentation-js) — `@marz32one/otel-rxjs-ws` (RxJS drop-in), `@marz32one/otel-ws` (native Node ws). |

**Dependencies (not submodules):** [dubonzi/otelresty](https://github.com/dubonzi/otelresty) — OTel for go-resty; API uses it for HTTP client spans + propagation to Worker.

After clone run `git submodule update --init` for both submodules.

---

## Environment variables

### API

| Variable                       | Default                    | Description                    |
|--------------------------------|----------------------------|--------------------------------|
| `NATS_URL`                     | `nats://localhost:4222`    | NATS address                  |
| `WORKER_URL`                   | `http://worker:8082`       | Worker base URL (for `/api/message-via-worker` via otelresty) |
| `MONGODB_URI`                  | `mongodb://localhost:27017`| MongoDB (for `/api/message-mongo`) |
| `OTEL_EXPORTER_OTLP_ENDPOINT`  | `localhost:4317`           | OTel Collector gRPC (use `otel-collector:4317` in Docker) |

### Worker

| Variable                       | Default                 | Description (set for spans in Tempo) |
|--------------------------------|-------------------------|--------------------------------------|
| `NATS_URL`                     | `nats://localhost:4222` | NATS address                         |
| `OTEL_EXPORTER_OTLP_ENDPOINT`  | `localhost:4317`        | Use `otel-collector:4317` in Docker  |

### dbwatcher

| Variable                       | Default                    |
|--------------------------------|----------------------------|
| `MONGODB_URI`                  | `mongodb://localhost:27017`|
| `NATS_URL`                     | `nats://localhost:4222`    |
| `OTEL_EXPORTER_OTLP_ENDPOINT`  | `localhost:4317`           |

### Frontend (build-time)

| Variable                   | Default                    | Description |
|----------------------------|----------------------------|-------------|
| `VITE_API_URL`             | `http://localhost:8088`   | API base URL |
| `VITE_WS_URL`              | `ws://localhost:8082`     | Worker WebSocket URL |
| `VITE_OTEL_COLLECTOR_URL`  | `http://localhost:4318`   | OTel Collector HTTP endpoint (CORS) |
| `VITE_OTEL_LOG_LEVEL`      | _(unset = silent)_        | Enable `diag` output from JS instrumentation packages: `debug`, `info`, `warn`, `error` |

### ws-node-backend

| Variable                          | Default                    | Description |
|-----------------------------------|----------------------------|-------------|
| `PORT`                            | `8085`                     | HTTP / WebSocket listen port |
| `OTEL_EXPORTER_OTLP_ENDPOINT`     | `http://localhost:4318`    | OTel Collector HTTP endpoint |
| `OTEL_LOG_LEVEL`                  | _(unset = silent)_         | Enable `diag` output from `@marz32one/otel-ws`: `debug`, `info`, `warn`, `error` |

---

## Troubleshooting

- **API traces disappear after Collector restart:** Restart dependent services: `make restart` (or `COMPOSE_CMD='docker compose' make restart`).
- **Worker spans missing in Tempo:** Set `OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317` in `docker-compose.yml`.
- **Frontend changes not applied:** `COMPOSE_CMD='docker compose' make build && COMPOSE_CMD='docker compose' make up` (or podman defaults: `make build && make up`).
- **Tempo returns 404 for trace:** Wait a few seconds after sending then query again.
- **Makefile with Docker:** `COMPOSE_CMD='docker compose' make up`.

---

## License

[Apache License 2.0](LICENSE)
