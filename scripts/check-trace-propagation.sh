#!/usr/bin/env bash
# Check trace propagation: (1) MongoDB path API → Tempo, (2) JetStream + WebSocket path
# (API → NATS → Worker → WebSocket broadcast) with a span named websocket.send in Tempo.
#
# The WebSocket check requires at least one connected client before POST /api/message;
# otherwise the worker never calls WriteMessage and no websocket.send span exists.
# Uses Node + `ws` from pkg/instrumentation-js (run: cd pkg/instrumentation-js && npm install).
#
# Assumes stack is already up (e.g. after 'make up'). Exits 0 if both checks pass.

set -e
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

API_URL="${API_URL:-http://localhost:8088}"
TEMPO_URL="${TEMPO_URL:-http://localhost:3200}"
WAIT_FLUSH="${WAIT_FLUSH:-15}"
# Worker WebSocket URL as seen from the host running this script (Docker maps 8082)
WS_URL="${WS_URL:-ws://127.0.0.1:8082/ws}"
# Set to 1 to skip JetStream/WebSocket verification (e.g. CI without npm install in instrumentation-js)
VERIFY_TRACE_SKIP_WS="${VERIFY_TRACE_SKIP_WS:-0}"

WS_HOLD_PID=""

cleanup() {
  if [ -n "${WS_HOLD_PID:-}" ] && kill -0 "$WS_HOLD_PID" 2>/dev/null; then
    kill "$WS_HOLD_PID" 2>/dev/null || true
    wait "$WS_HOLD_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

# Query Tempo for trace JSON; echoes body to stdout.
query_tempo_trace() {
  local trace_id="$1"
  local now start end url
  now=$(date +%s)
  start=$((now - 120))
  end=$((now + 10))
  url="${TEMPO_URL}/api/traces/${trace_id}?start=${start}&end=${end}"
  curl -s -H "Accept: application/json" "$url"
}

# Returns 0 if response looks like a valid trace payload.
trace_response_ok() {
  local json="$1"
  if echo "$json" | grep -qE '"traceId"|"spans"|"scopeSpans"|"batches"'; then
    return 0
  fi
  return 1
}

# Start a background WebSocket client so worker broadcast creates websocket.send spans.
start_ws_holder() {
  local jsroot="$REPO_ROOT/pkg/instrumentation-js"
  # npm workspaces hoist dependencies to instrumentation-js/node_modules/
  if [ ! -d "$jsroot/node_modules/ws" ]; then
    echo "  FAIL: WebSocket hold needs the ws package (npm workspaces install under instrumentation-js)."
    echo "        Run: cd pkg/instrumentation-js && npm install"
    echo "        Or set VERIFY_TRACE_SKIP_WS=1 to skip the JetStream/WebSocket check."
    return 1
  fi
  if ! command -v node >/dev/null 2>&1; then
    echo "  FAIL: node is required for WebSocket hold (or set VERIFY_TRACE_SKIP_WS=1)"
    return 1
  fi
  (
    cd "$jsroot"
    export WS_URL
    node -e '
      const WebSocket = require("ws");
      const u = process.env.WS_URL || "ws://127.0.0.1:8082/ws";
      const ws = new WebSocket(u);
      ws.on("open", () => {});
      ws.on("error", (e) => {
        console.error("verify-trace ws:", e.message);
        process.exit(1);
      });
      setInterval(() => {}, 3600000);
    '
  ) &
  WS_HOLD_PID=$!
  sleep 2
  if ! kill -0 "$WS_HOLD_PID" 2>/dev/null; then
    echo "  FAIL: WebSocket holder exited (is worker up on ${WS_URL}?)"
    return 1
  fi
  echo "  OK: WebSocket client connected (holder pid $WS_HOLD_PID)"
  return 0
}

echo "=== Trace propagation check ==="
echo "  API:   $API_URL"
echo "  Tempo: $TEMPO_URL"

# --- 1) MongoDB path (API → Mongo → dbwatcher → NATS → Worker; trace in Tempo) ---
echo ""
echo "--- Path 1: MongoDB (API → Mongo → Tempo) ---"
resp=$(curl -s -w "\n%{http_code}" -X POST "$API_URL/api/message-mongo" \
  -H "Content-Type: application/json" \
  -d '{"text":"check-trace-propagation-mongo"}')
code=$(echo "$resp" | tail -n 1)
body=$(echo "$resp" | sed '$d')
if [ "$code" != "200" ]; then
  echo "  FAIL: POST /api/message-mongo returned HTTP $code"
  echo "$body"
  exit 1
fi
echo "  OK: API accepted request (HTTP 200)"

TRACE_ID_MONGO=$(echo "$body" | sed -n 's/.*"trace_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
if [ -z "$TRACE_ID_MONGO" ]; then
  echo "  FAIL: No trace_id in API response: $body"
  exit 1
fi
echo "  trace_id from API: $TRACE_ID_MONGO"

echo "  Waiting ${WAIT_FLUSH}s for trace flush..."
sleep "$WAIT_FLUSH"

trace_json=$(query_tempo_trace "$TRACE_ID_MONGO")
if [ -z "$trace_json" ]; then
  echo "  FAIL: Tempo returned empty response for trace $TRACE_ID_MONGO"
  exit 1
fi
if ! trace_response_ok "$trace_json"; then
  echo "  FAIL: Tempo response does not look like a trace for $TRACE_ID_MONGO"
  echo "$trace_json" | head -c 500
  echo ""
  exit 1
fi
echo "  OK: Tempo returned trace for trace_id $TRACE_ID_MONGO"

if command -v jq >/dev/null 2>&1; then
  span_count=0
  if echo "$trace_json" | jq -e '.batches' >/dev/null 2>&1; then
    span_count=$(echo "$trace_json" | jq '[.batches[].scopeSpans[].spans[]] | length' 2>/dev/null || echo "0")
  elif echo "$trace_json" | jq -e '.trace.resourceSpans' >/dev/null 2>&1; then
    span_count=$(echo "$trace_json" | jq '[.trace.resourceSpans[].scopeSpans[].spans[]] | length' 2>/dev/null || echo "0")
  elif echo "$trace_json" | jq -e '.resourceSpans' >/dev/null 2>&1; then
    span_count=$(echo "$trace_json" | jq '[.resourceSpans[].scopeSpans[].spans[]] | length' 2>/dev/null || echo "0")
  fi
  if [ -n "$span_count" ] && [ "${span_count:-0}" -gt 0 ]; then
    echo "  OK: Trace has $span_count span(s)"
  else
    echo "  WARN: Could not count spans (jq path may vary); trace presence accepted."
  fi
fi

# --- 2) JetStream + WebSocket (API → NATS → Worker → WebSocket; websocket.send in same trace) ---
echo ""
echo "--- Path 2: JetStream + WebSocket (API → NATS → Worker → WS → Tempo) ---"

if [ "$VERIFY_TRACE_SKIP_WS" = "1" ]; then
  echo "  SKIP: VERIFY_TRACE_SKIP_WS=1 (JetStream/WebSocket check not run)"
else
  if ! start_ws_holder; then
    exit 1
  fi

  resp=$(curl -s -w "\n%{http_code}" -X POST "$API_URL/api/message" \
    -H "Content-Type: application/json" \
    -d '{"text":"check-trace-propagation-jetstream-ws"}')
  code=$(echo "$resp" | tail -n 1)
  body=$(echo "$resp" | sed '$d')
  if [ "$code" != "200" ]; then
    echo "  FAIL: POST /api/message returned HTTP $code"
    echo "$body"
    exit 1
  fi
  echo "  OK: POST /api/message accepted (HTTP 200)"

  TRACE_ID_WS=$(echo "$body" | sed -n 's/.*"trace_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
  if [ -z "$TRACE_ID_WS" ]; then
    echo "  FAIL: No trace_id in API response: $body"
    exit 1
  fi
  echo "  trace_id from API: $TRACE_ID_WS"

  echo "  Waiting ${WAIT_FLUSH}s for trace flush..."
  sleep "$WAIT_FLUSH"

  trace_json_ws=$(query_tempo_trace "$TRACE_ID_WS")
  if [ -z "$trace_json_ws" ]; then
    echo "  FAIL: Tempo returned empty response for trace $TRACE_ID_WS"
    exit 1
  fi
  if ! trace_response_ok "$trace_json_ws"; then
    echo "  FAIL: Tempo response does not look like a trace for $TRACE_ID_WS"
    echo "$trace_json_ws" | head -c 500
    echo ""
    exit 1
  fi
  if ! echo "$trace_json_ws" | grep -q 'websocket.send'; then
    echo "  FAIL: Trace for JetStream path has no span name websocket.send"
    echo "        (Worker must broadcast to at least one WS client; check WS_URL=$WS_URL)"
    exit 1
  fi
  echo "  OK: Tempo trace contains websocket.send (Worker WebSocket broadcast)"
fi

echo ""
echo "=== Trace propagation check: done ==="
