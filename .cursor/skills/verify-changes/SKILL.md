---
name: verify-changes
description: Enforces build-and-test verification after every code or config change in this OTel traces project (api, worker, frontend, pkg/nats.go, Docker, OTel/Tempo). Ensures Go builds pass, services stay healthy, and trace pipeline works. Use when the user says verify/test, or when modifying any project file so that each change is self-verified before marking work done.
---

# Verify Changes

**When to run**: After **every** code or config change in this repo (api, worker, frontend, pkg/natstrace, pkg/nats.go, Docker, OTel/Tempo). The agent must self-verify before marking any task done.

Every change MUST pass a verification cycle before completion. Never consider a task done on code edits alone.

**Go files**: Whenever **any `.go` file** is modified, you **must** run **unit tests** and **golangci-lint** for the affected Go module(s) before marking the task done. If tests or lint fail, fix and re-run until both pass.

## Core Rule

```
EDIT → BUILD → RUN → TEST → (fix if broken) → DONE
```

If any step fails, fix and restart from BUILD. Do not skip steps.

## Project Context (from README)

- **API** (Go, :8081): receives HTTP, publishes to NATS JetStream; two paths: `POST /api/message` (built-in JetStreamContext), `POST /api/message-v2` (jetstream pkg).
- **Worker** (Go, :8082): subscribes to NATS, broadcasts via WebSocket.
- **Frontend** (:3000): React + OTel; sends traceparent to API; exports OTLP to Collector.
- **Trace path**: Frontend → API (otelgin + producer span) → OTel Collector → Tempo (:3200). Producer span name is `send <subject>` (e.g. `send messages.new`).
- **Build**: `api` and `worker` use `replace` to `pkg/nats.go`; their Dockerfiles use repo root as build context.

## Project Behaviour Requirements

- **Message display**: 每則使用者輸入的訊息在訊息列表（textarea）中必須**只顯示一次**，即該次輸入的文字一則、不可重複。若任一按鈕（JetStream 內建 / jetstream 套件 / Core NATS）送出後同一則文字出現兩次或以上，即為不符合專案要求，需修正（例如 API 與 worker 的 subject 設計須確保每則訊息只被 broadcast 一次）。

## Verification by Changed Paths

Decide what to verify from which files were edited. Then run the matching steps.

### 1. Go — API (`api/**/*.go`, `api/go.mod`, `api/go.sum`)

| Step | Command / check |
|------|------------------|
| Tidy (if go.mod/sum changed) | `cd api && go mod tidy` |
| Build | `cd api && go build -o /dev/null .` |
| Tests | `cd api && go test ./...` (if the module has tests) |
| Lint | ReadLints on edited `api/**` files; if `.golangci.yml` exists in api, run `golangci-lint run ./...` |
| Run (if compose up) | `docker compose up -d --build api` then `docker compose logs api --tail 10` |

If `api/main.go` was changed, also run **End-to-End Trace Verification** below.

### 2. Go — Worker (`worker/**/*.go`, `worker/go.mod`, `worker/go.sum`)

| Step | Command / check |
|------|------------------|
| Tidy (if go.mod/sum changed) | `cd worker && go mod tidy` |
| Build | `cd worker && go build -o /dev/null .` |
| Tests | `cd worker && go test ./...` (if the module has tests) |
| Lint | ReadLints on edited `worker/**` files; if `.golangci.yml` exists in worker, run `golangci-lint run ./...` |
| Run (if compose up) | `docker compose up -d --build worker` then `docker compose logs worker --tail 10` |

### 3. Go — natstrace (`pkg/natstrace/**/*.go`)

| Step | Command / check |
|------|------------------|
| Tidy (if go.mod/sum changed) | `cd pkg/natstrace && go mod tidy` |
| Build | `cd pkg/natstrace && go build ./...` |
| **Unit tests** | `cd pkg/natstrace && go test ./...` — **must pass** |
| **golangci-lint** | `cd pkg/natstrace && golangci-lint run ./...` — **must pass** (install via `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest` if missing) |
| Lint | ReadLints on edited `pkg/natstrace/**` files |

### 4. Shared Go — NATS (`pkg/nats.go/**`)

Both API and Worker depend on this (replace in go.mod). Verify **both** modules:

```bash
cd api    && go build -o /dev/null . && \
cd ../worker && go build -o /dev/null .
```

If compose is up: `docker compose up -d --build api worker`.

### 5. Frontend (`frontend/src/**`, `frontend/package.json`, `frontend/vite.config.js`, etc.)

| Step | Command / check |
|------|------------------|
| Lint | ReadLints on edited frontend files |
| Run (if compose up) | `docker compose up -d --build frontend` then `sleep 3` and `curl -s -o /dev/null -w "%{http_code}" http://localhost:3000/` → expect 200 |

If `frontend/src/App.tsx` or `frontend/src/tracing.ts` was changed, also run **End-to-End Trace Verification**.

### 6. Docker / Infra (`docker-compose.yml`, `**/Dockerfile`, `tempo.yaml`, `otel-collector-config.yaml`, `nats/**`, `grafana/**`)

| Step | Command / check |
|------|------------------|
| Up | `docker compose up -d` (add `--build` if Dockerfile or compose changed) |
| All Up | `docker compose ps` — no service in Exit/Failed; grep for non-"Up" status should show no service rows |
| Logs | For any touched service: `docker compose logs --tail 5 <service>` and ensure no `error`/`fatal`/`panic` |

If `docker-compose.yml`, `otel-collector-config.yaml`, or `tempo.yaml` was changed, also run **End-to-End Trace Verification**.

## End-to-End Trace Verification

Run when changes touch **any** of: `api/main.go`, `frontend/src/App.tsx`, `frontend/src/tracing.ts`, `otel-collector-config.yaml`, `tempo.yaml`, `docker-compose.yml`.

1. **Trigger a trace** (valid 32-char hex trace ID so Tempo accepts the query):

```bash
TRACE_ID="deadbeef000000000000000000000001"
SPAN_ID="1234567890abcdef"
curl -s http://localhost:8081/api/message -X POST \
  -H "Content-Type: application/json" \
  -H "traceparent: 00-${TRACE_ID}-${SPAN_ID}-01" \
  -d '{"text":"skill-verify-test"}'
# Must return: {"status":"published"}
```

2. **Optional**: Same for v2 endpoint to confirm both paths work:

```bash
curl -s http://localhost:8081/api/message-v2 -X POST \
  -H "Content-Type: application/json" \
  -H "traceparent: 00-${TRACE_ID}-${SPAN_ID}-01" \
  -d '{"text":"skill-verify-v2"}'
# Must return: {"status":"published"}
```

3. **Query Tempo** (wait for flush):

```bash
sleep 5
curl -s "http://localhost:3200/api/traces/${TRACE_ID}"
```

4. **Assert**: Response JSON contains spans from service `api`, including:
   - A producer span (e.g. `send messages.new`, `send <subject>`, or `publish-to-nats`),
   - And an HTTP span like `POST /api/message` (or `/api/message-v2`).

If the trace query returns 404 or no api spans, the tracing pipeline is broken — debug before proceeding.

## Failure Response Protocol

1. **Read the error** — build output, logs, or HTTP response.
2. **Diagnose** — root cause from actual message, not guesswork.
3. **Fix** — minimal change to resolve.
4. **Re-verify** — run the full verification for the affected file type again.
5. **Never skip** — do not claim "it should work" without running the steps and showing success.

## What Counts as Verified

| Check | Evidence |
|-------|----------|
| Go build | Exit code 0 for `go build -o /dev/null .` (or `go build ./...` in pkg/natstrace) in the right module |
| Go unit tests | Exit code 0 for `go test ./...` in the changed module (required when any .go file was edited) |
| Go lint | Exit code 0 for `golangci-lint run ./...` where the module has `.golangci.yml` (e.g. pkg/natstrace) |
| Services running | `docker compose ps` — all relevant services "Up" |
| API | `curl` to `/api/message`, `/api/message-v2`, or `/api/message-core` returns `{"status":"published"}` with HTTP 200 |
| Frontend | `curl -s -o /dev/null -w "%{http_code}" http://localhost:3000/` returns 200 |
| Trace pipeline | Tempo `GET /api/traces/<TRACE_ID>` returns JSON with api spans (e.g. producer span + `POST /api/message`) |
| Message display | One send → one line in message list (no duplicate; see **Project Behaviour Requirements**) |
| No regressions | No new errors in logs; previously-working services still running |

## Quick Reference: One-Shot Full Stack Check

When many areas changed or in doubt, run a full pass:

```bash
# Go: natstrace (if pkg/natstrace or Go tracing code was touched)
cd pkg/natstrace && go test ./... && golangci-lint run ./... && cd ../..

# Build
cd api    && go build -o /dev/null . && cd ../worker && go build -o /dev/null .

# Stack
docker compose up -d --build api frontend worker

# Health
sleep 3
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:3000/
curl -s http://localhost:8081/api/message -X POST -H "Content-Type: application/json" -d '{"text":"ok"}'
curl -s http://localhost:8081/api/message-v2 -X POST -H "Content-Type: application/json" -d '{"text":"ok"}'
curl -s http://localhost:8081/api/message-core -X POST -H "Content-Type: application/json" -d '{"text":"ok"}'

# E2E trace (then query Tempo after sleep 5)
TRACE_ID="deadbeef000000000000000000000002"
SPAN_ID="1234567890abcdef"
curl -s http://localhost:8081/api/message -X POST -H "Content-Type: application/json" -H "traceparent: 00-${TRACE_ID}-${SPAN_ID}-01" -d '{"text":"e2e"}'
sleep 5 && curl -s "http://localhost:3200/api/traces/${TRACE_ID}" | head -c 500
```

All steps must succeed for the change to be considered verified.
