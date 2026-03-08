# OTel Traces Test

[繁體中文 (Traditional Chinese)](README.zh-TW.md)

---

A full-stack example demonstrating **OpenTelemetry distributed tracing**. Users send messages from the browser over three paths: **JetStream** (API publishes to NATS), **Core NATS** (fire-and-go), and **MongoDB** (API writes to Mongo → dbwatcher watches change stream → forwards to NATS → Worker). The Worker subscribes and broadcasts back to the frontend over WebSocket; **one trace runs from frontend send to frontend receive** across all services and can be viewed in Grafana/Tempo.

---

## Summary for first-time users and AI agents

- **Goal:** End-to-end W3C Trace Context propagation: browser → API (HTTP + NATS publish) → Worker (NATS subscribe + WebSocket broadcast) → browser (WebSocket receive and final span).
- **Stack:** React 18 + TypeScript + Vite (frontend; **Grafana Faro SDK** for trace and W3C propagation), Go + Gin (API), Go + gorilla/websocket (Worker), NATS (JetStream + Core), OpenTelemetry SDK, OTel Collector, Grafana Tempo, Grafana.
- **Three paths:** **JetStream** (`POST /api/message` → `messages.new` → Worker), **Core NATS** (`POST /api/message-core` → `messages.core` → Worker), **MongoDB** (`POST /api/message-mongo` → API writes to Mongo → **dbwatcher** watches change stream → publishes `messages.db` → Worker). One trace flows from API to Worker; query by trace ID in Tempo.
- **Submodule:** `pkg/natstrace` ([natstrace](https://github.com/Marz32onE/natstrace) — OTel wrapper for NATS, W3C + JetStream/Core). Run `git submodule update --init` after clone.
- **Build & run:** Docker Compose builds from repo root; `api` and `worker` use root as build context. Use `make up` or `docker compose up --build` (Makefile defaults to `podman compose`; override with `COMPOSE_CMD='docker compose'`).
- **Config:** `api` and `worker` need `OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317` to send spans to Tempo; frontend uses `VITE_OTEL_COLLECTOR_URL` for OTLP/HTTP via Collector (CORS) to Tempo.

---

## Architecture

```
┌──────────┐   HTTP POST    ┌──────────┐   NATS JetStream   ┌──────────┐
│          │  (traceparent) │          │   messages.new     │          │
│ Frontend ├───────────────►│   API    ├───────────────────►│  Worker  │
│ :3000    │                │  :8088   │   messages.db      │  :8082   │
│          │  /message-mongo│     │    │  (trace context)  │     ▲    │
│          │                │     ▼    │                    │     │    │
│          │                │ ┌────────┐   change stream    │     │    │
│          │                │ │MongoDB │──► dbwatcher ──────┼─────┘    │
│          │                │ │ :27017 │   (→ messages.db) │          │
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
| **API**          | Go (Gin)                 | Endpoints: `POST /api/message` (JetStream), `POST /api/message-core` (Core), `POST /api/message-mongo` (MongoDB Insert), `...-mongo-update`, `...-mongo-read`, `...-mongo-delete`; returns `trace_id` | `8088`                                 |
| **Worker**       | Go (gorilla/websocket)   | JetStream (`messages.new`, `messages.db`) and Core Subscribe; broadcasts **traceparent/tracestate + body + api** as JSON over WebSocket  | `8082`                                 |
| **MongoDB**      | Mongo 7 (replica set)    | Stores `messaging.messages`; mongotrace injects `_oteltrace` on write. dbwatcher uses change stream (replica set required).             | `27017`                                |
| **dbwatcher**    | Go + mongo-driver        | Watches `messaging.messages` **CRUD** (insert, update, replace, delete); forwards to NATS JetStream `messages.db` (insert/update/replace: doc text; delete: `{"op":"delete","id":"..."}`). Worker subscribes and broadcasts. | no public port                         |
| **NATS**         | NATS Alpine + JetStream  | Message queue; healthcheck uses `wget` (alpine)                                                                                         | `4222` (client), `8222` (monitoring)   |
| **OTel Collector** | OTel Collector Contrib | Receives gRPC/HTTP OTLP, forwards to Tempo; HTTP endpoint has CORS for browser                                                          | `4317` (gRPC), `4318` (HTTP)           |
| **Tempo**        | Grafana Tempo 2.9.0      | Trace backend (**pinned 2.9.0**; v2.10+ incompatible)                                                                                    | `3200`                                 |
| **Grafana**      | Grafana latest           | UI; anonymous Admin; Tempo datasource configured                                                                                        | `3001`                                 |

---

## Message flow

1. User enters a message in **NATS** or **MongoDB** group and clicks the relevant button (JetStream, Core NATS, or MongoDB Insert/Update/Read/Delete). MongoDB actions use an editable **ID** field (default `_id`; after Insert the returned id is synced).
2. Frontend creates the appropriate send span (CLIENT), propagates via `traceparent`/`tracestate` headers, and sends HTTP POST to API.
3. API’s `otelgin` continues the trace; **JetStream** path uses `jetstreamtrace.Publish`; **Core** uses `natstrace.Publish`; **MongoDB** path uses mongotrace Collection (Insert/Update/Read/Delete) and returns `trace_id`.
4. **MongoDB path:** dbwatcher watches the collection’s change stream for **all CRUD** (insert, update, replace, delete). On insert/update/replace it publishes the document’s `text` to JetStream topic `messages.db` (with `_oteltrace` when present). On delete it publishes `{"op":"delete","id":"<hex>"}`. Worker subscribes and broadcasts **traceparent, tracestate, body, api** over WebSocket.
5. **JetStream/Core:** Worker receives via JetStream (Consume/Messages) or Core Subscribe; `natstrace`/`jetstreamtrace` extract trace from headers, create receive span, and broadcast over WebSocket.
6. Frontend receives via WebSocket; the **shared results area** at the bottom (“由 WebSocket / Worker 監聽 NATS 取出的結果”) shows all messages. If `traceparent` and `body` exist, frontend uses `propagation.extract` and creates `receive message` span (CONSUMER). UI shows **Trace verification** and last `trace_id` for Grafana/Tempo.

---

## Trace flow (in Grafana)

**JetStream path** (Core uses `messages.core`):

```
Frontend: send-message-jetstream          (SpanKind CLIENT)
  └─ API: POST /api/message               (otelgin, SERVER)
       └─ API: messages.new publish       (jetstreamtrace, PRODUCER)
            └─ Worker: messages.new receive (jetstreamtrace, CONSUMER)
                 └─ Frontend: receive message (CONSUMER)
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
- **API/Worker:** OTLP/gRPC → OTel Collector → Tempo.
- API CORS allows `traceparent`, `tracestate`.

---

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) + [Docker Compose](https://docs.docker.com/compose/) (or Podman Compose; see Makefile)
- [Git](https://git-scm.com/) (with submodule support)

### Development: tests and lint

- Go tests use **testify** (`require`/`assert`) in `pkg/natstrace`, `pkg/mongodbtrace`.
- Run **`go vet ./...`** and **`go test ./...`** in each module (`api`, `worker`, `dbwatcher`, `pkg/natstrace`, `pkg/mongodbtrace`).
- With [golangci-lint](https://golangci-lint.run/) installed, run it per Go module directory (or `go work` at root).

---

## Quick start

```bash
git clone --recurse-submodules git@github.com:Marz32onE/otel-traces-test.git
cd otel-traces-test

# If already cloned without submodules
git submodule update --init

# Start all services
docker compose up --build
# Or: make up   (default: podman compose; use COMPOSE_CMD='docker compose' for Docker)
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

Requires `go`, `docker`, `docker compose`. The script builds, brings up Compose, hits all API endpoints, and checks dbwatcher/worker logs and Tempo.

Manual MongoDB path check:

```bash
curl -s -X POST http://localhost:8088/api/message-mongo -H "Content-Type: application/json" -d '{"text":"mongo-e2e"}'
docker compose logs dbwatcher --tail 20
docker compose logs worker --tail 20
```

### Stop and clean

```bash
docker compose down
docker compose down -v   # with volumes
make down
make clean   # with -v
```

---

## Project structure

```
.
├── api/              # Go + Gin API (three endpoints, trace_id)
├── worker/           # WebSocket worker (NATS subscribe + broadcast)
├── dbwatcher/        # Mongo change stream → NATS
├── frontend/         # React + Vite + Grafana Faro
├── pkg/
│   ├── natstrace/    # Git submodule — NATS OTel (W3C + JetStream/Core)
│   └── mongodbtrace/ # mongotrace — MongoDB OTel (InitTracer before NewClient)
├── charts/otel-traces-test/config/
├── grafana/
├── docker-compose.yml
├── Makefile
└── LICENSE
```

`api` and `worker` Dockerfiles use the repo root as build context.

### Tracing init and API (natstrace / mongodbtrace)

- **natstrace:** Call **`natstrace.InitTracer(endpoint, attrs...)`** before **`natstrace.Connect`**. Otherwise `Connect` returns `ErrInitTracerRequired`. Use **`defer natstrace.ShutdownTracer()`** on exit.
- **mongodbtrace:** Call **`mongotrace.InitTracer(endpoint, attrs...)`** before **`mongotrace.NewClient(uri)`**. Otherwise `NewClient` returns **`ErrInitTracerRequired`**. Use **`defer mongotrace.ShutdownTracer()`** on exit.
- **API:** `natstrace.Connect(url, natsOpts)` — no `WithTracerProvider`/`WithPropagator`; tracer from global. `mongotrace.NewClient(uri)` only accepts **`WithOtelMongoOptions(...)`**.
- **Tests:** Use **`natstrace.InitTracer("", natstrace.WithTracerProvider(tp))`** or **`mongotrace.InitTracer("", mongotrace.WithTracerProvider(tp))`** (and optionally `otel.SetTextMapPropagator(prop)` for natstrace) before `Connect`/`NewClient`.

---

## Git submodules

| Path             | Description                                      |
|------------------|--------------------------------------------------|
| `pkg/natstrace`  | [natstrace](https://github.com/Marz32onE/natstrace) — NATS OTel; use tag v0.1.4+ |

`api` and `worker` use `replace github.com/Marz32onE/natstrace => ../pkg/natstrace` for local development. After clone run `git submodule update --init`.

---

## Environment variables

### API

| Variable                       | Default                    | Description                    |
|--------------------------------|----------------------------|--------------------------------|
| `NATS_URL`                     | `nats://localhost:4222`    | NATS address                  |
| `MONGODB_URI`                  | `mongodb://localhost:27017`| MongoDB (for `/api/message-mongo`) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317`           | OTel Collector gRPC (use `otel-collector:4317` in Docker) |

### Worker

| Variable                       | Default                 | Description (set for spans in Tempo) |
|--------------------------------|-------------------------|--------------------------------------|
| `NATS_URL`                     | `nats://localhost:4222` | NATS address                         |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317`        | Use `otel-collector:4317` in Docker  |

### dbwatcher

| Variable                       | Default                    |
|--------------------------------|----------------------------|
| `MONGODB_URI`                  | `mongodb://localhost:27017`|
| `NATS_URL`                     | `nats://localhost:4222`    |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317`           |

### Frontend (build-time)

| Variable                   | Default                    |
|----------------------------|----------------------------|
| `VITE_API_URL`             | `http://localhost:8088`   |
| `VITE_WS_URL`              | `ws://localhost:8082`      |
| `VITE_OTEL_COLLECTOR_URL`  | `http://localhost:4318`   |

---

## Troubleshooting

- **API traces disappear after Collector restart:** Restart dependent services: `docker compose restart otel-collector api worker`.
- **Worker spans missing in Tempo:** Set `OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317` in `docker-compose.yml`.
- **Frontend changes not applied:** `docker compose build --no-cache frontend && docker compose up -d frontend`.
- **Tempo returns 404 for trace:** Wait a few seconds after sending then query again.
- **Makefile with Docker:** `COMPOSE_CMD='docker compose' make up`.

---

## License

[Apache License 2.0](LICENSE)
