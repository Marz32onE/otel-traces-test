---
name: verify-changes
description: Enforces a test-and-debug cycle after every code change. Automatically verifies builds, service health, and end-to-end functionality before marking work as done. Use proactively on ALL tasks that modify code, config, or infrastructure files.
---

# Verify Changes

Every change MUST pass through a verification cycle before completion. Never consider a task done based on code edits alone.

## Core Rule

```
EDIT → BUILD → RUN → TEST → (fix if broken) → DONE
```

If any step fails, fix the issue and restart the cycle from BUILD. Do NOT skip steps.

## Verification by File Type

### Go files (`api/**/*.go`, `worker/**/*.go`)

1. **Build**: `cd <module> && go build -o /dev/null .`
2. **Lint**: Run ReadLints on edited files
3. **If docker is running**: restart the affected service and check logs

```bash
# Example
cd api && go build -o /dev/null .
# If compose is up:
docker compose restart api && sleep 2 && docker compose logs api --tail 10
```

### Frontend files (`frontend/src/**/*.ts`, `frontend/src/**/*.tsx`)

1. **Lint**: Run ReadLints on edited files
2. **If docker is running**: rebuild and verify

```bash
docker compose up -d --build frontend && sleep 3
curl -s -o /dev/null -w "%{http_code}" http://localhost:3000/
```

### Docker / Infrastructure (`docker-compose.yml`, `tempo.yaml`, `otel-collector-config.yaml`, `**/Dockerfile`)

1. **Restart affected services**: `docker compose up -d` (add `--build` if Dockerfile changed)
2. **Health check**: ALL services must be running

```bash
docker compose ps --format "table {{.Name}}\t{{.Status}}" | grep -v "Up" | grep -v "NAME"
# ^ This must produce empty output (no crashed services)
```

3. **Check logs for errors**:

```bash
docker compose logs --tail 5 <service> 2>&1 | grep -i "error\|fatal\|panic"
```

### Go module files (`go.mod`, `go.sum`)

1. **Tidy**: `go mod tidy`
2. **Build**: `go build -o /dev/null .`

## End-to-End Trace Verification

When changes touch **any** of these: `api/main.go`, `frontend/src/tracing.ts`, `frontend/src/App.tsx`, `otel-collector-config.yaml`, `tempo.yaml`, `docker-compose.yml`:

```bash
# 1. Send a test message with known trace ID
curl -s http://localhost:8081/api/message -X POST \
  -H "Content-Type: application/json" \
  -H "traceparent: 00-test0000000000000000000000000000-1234567890abcdef-01" \
  -d '{"text":"skill-verify-test"}'
# Must return: {"status":"published"}

# 2. Wait for ingestion, then query Tempo
sleep 5
curl -s http://localhost:3200/api/traces/test0000000000000000000000000000
# Must return JSON with "publish-to-nats" span
```

If the trace query returns 404 or empty, the tracing pipeline is broken — debug before proceeding.

## Failure Response Protocol

When a verification step fails:

1. **Read the error** — check logs, build output, or HTTP response
2. **Diagnose** — identify root cause (don't guess, read the actual error)
3. **Fix** — make the minimal change to resolve the issue
4. **Re-verify** — restart the full verification cycle for the file type
5. **Never skip** — do not tell the user "it should work" without proof

## What Counts as "Verified"

| Check | Evidence required |
|---|---|
| Go build | Exit code 0, no output |
| Service running | `docker compose ps` shows "Up" |
| API works | `curl` returns expected JSON with HTTP 200 |
| Frontend serves | `curl` returns HTTP 200 on `localhost:3000` |
| Trace pipeline | Tempo API returns spans with correct trace ID |
| No regressions | All previously-working services still running |
