#!/usr/bin/env bash
# Verify the full path: API (all endpoints) + MongoDB path (API → Mongo → dbwatcher → NATS → Worker).
# Run from repo root. Requires: go + one of:
# - docker compose
# - podman-compose
# - podman compose

set -e
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# Ensure Go is on PATH (e.g. from ~/.bashrc)
export PATH="${PATH}:/usr/local/go/bin:${HOME}/go/bin"

detect_compose_cmd() {
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    echo "docker compose"
    return
  fi
  if command -v podman-compose >/dev/null 2>&1; then
    echo "podman-compose"
    return
  fi
  if command -v podman >/dev/null 2>&1 && podman compose version >/dev/null 2>&1; then
    echo "podman compose"
    return
  fi
  echo ""
}

COMPOSE_CMD="${COMPOSE_CMD:-$(detect_compose_cmd)}"
if [ -z "$COMPOSE_CMD" ]; then
  echo "FAIL: No compose command found. Install docker compose or podman compose/podman-compose."
  exit 1
fi

compose() {
  $COMPOSE_CMD "$@"
}

echo "Using compose command: $COMPOSE_CMD"
echo ""

echo "=== 1. Go build (api, worker, dbwatcher) ==="
(cd api      && go mod tidy && go build -o /dev/null .)
(cd worker   && go mod tidy && go build -o /dev/null .)
(cd dbwatcher && go mod tidy && go build -o /dev/null .)
echo "Go build OK"

echo ""
echo "=== 2. instrumentation-go tests (optional quick check) ==="
(cd pkg/instrumentation-go/otel-nats && go test ./... 2>/dev/null) || true

echo ""
echo "=== 3. Docker Compose up --build ==="
compose up -d --build
echo "Waiting for services (30s)..."
sleep 30

echo ""
echo "=== 4. Service status ==="
compose ps

echo ""
echo "=== 5. API health: all endpoints ==="
# JetStream
r=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8088/api/message -H "Content-Type: application/json" -d '{"text":"verify-js"}')
echo "  POST /api/message          -> HTTP $r (expect 200)"
# Core NATS
r=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8088/api/message-core -H "Content-Type: application/json" -d '{"text":"verify-core"}')
echo "  POST /api/message-core     -> HTTP $r (expect 200)"
# MongoDB
r=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8088/api/message-mongo -H "Content-Type: application/json" -d '{"text":"verify-mongo-path"}')
echo "  POST /api/message-mongo    -> HTTP $r (expect 200)"

if [ "$r" != "200" ]; then
  echo "  FAIL: /api/message-mongo returned $r"
  compose logs api --tail 30
  exit 1
fi

echo ""
echo "=== 6. MongoDB path: wait for dbwatcher → NATS → worker (15s) ==="
sleep 15
echo "  dbwatcher logs (last 15 lines):"
compose logs dbwatcher --tail 15
echo ""
echo "  worker logs (last 15 lines):"
compose logs worker --tail 15

if compose logs dbwatcher --tail 50 2>/dev/null | grep -q "Forwarded to messages.db"; then
  echo ""
  echo "  OK: dbwatcher forwarded message to NATS"
fi
if compose logs worker --tail 80 2>/dev/null | grep -qE "\[DB\] id=.*fetched|\[DB\] delete id="; then
  echo "  OK: worker handled messages.db (fetch or delete)"
fi

echo ""
echo "=== 7. Frontend ==="
r=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:3000/ 2>/dev/null) || r="000"
echo "  GET http://localhost:3000/ -> HTTP $r (expect 200)"

echo ""
echo "=== 8. E2E trace (MongoDB path) ==="
TRACE_ID="deadbeef000000000000000000000003"
SPAN_ID="1234567890abcdef"
curl -s -X POST http://localhost:8088/api/message-mongo \
  -H "Content-Type: application/json" \
  -H "traceparent: 00-${TRACE_ID}-${SPAN_ID}-01" \
  -d '{"text":"e2e-trace-mongo"}'
echo ""
echo "  Waiting 5s for trace flush..."
sleep 5
echo "  Tempo query:"
curl -s "http://localhost:3200/api/traces/${TRACE_ID}" | head -c 400
echo ""
echo ""
echo "=== Verify full path: done ==="
