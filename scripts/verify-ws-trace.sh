#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

WS_NODE_URL="${WS_NODE_URL:-http://localhost:8085}"
WS_URL="${WS_URL:-ws://localhost:8085/otel-ws}"
TEMPO_URL="${TEMPO_URL:-http://localhost:3200}"
WAIT_FLUSH="${WAIT_FLUSH:-10}"

echo "=== WS trace verification ==="
echo "  WS node: ${WS_NODE_URL}"
echo "  Tempo:   ${TEMPO_URL}"

echo ""
echo "--- Waiting for ws-node-backend health ---"
for i in $(seq 1 30); do
  if curl -fsS "${WS_NODE_URL}/health" >/dev/null 2>&1; then
    echo "  OK: ws-node-backend is healthy"
    break
  fi
  sleep 1
  if [ "$i" = "30" ]; then
    echo "  FAIL: ws-node-backend did not become healthy in time"
    exit 1
  fi
done

echo ""
echo "--- Triggering WS trace propagation and querying Tempo ---"
(
  cd "${REPO_ROOT}/pkg/instrumentation-js"
  node --no-warnings ./verify-ws-trace.mjs \
    --wsUrl "${WS_URL}" \
    --tempoUrl "${TEMPO_URL}" \
    --waitFlush "${WAIT_FLUSH}"
)

echo ""
echo "=== WS trace verification: done ==="

