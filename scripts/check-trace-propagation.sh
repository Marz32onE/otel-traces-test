#!/usr/bin/env bash
# Check that the whole trace propagation path works: API → OTLP → Tempo (trace queryable by ID).
# Assumes stack is already up (e.g. after 'make up'). Exits 0 if trace is found in Tempo, 1 otherwise.

set -e
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

API_URL="${API_URL:-http://localhost:8088}"
TEMPO_URL="${TEMPO_URL:-http://localhost:3200}"
WAIT_FLUSH="${WAIT_FLUSH:-15}"

echo "=== Trace propagation check ==="
echo "  API:   $API_URL"
echo "  Tempo: $TEMPO_URL"

# 1. Trigger a request; API returns the trace_id it is exporting (same trace as its spans).
resp=$(curl -s -w "\n%{http_code}" -X POST "$API_URL/api/message-mongo" \
  -H "Content-Type: application/json" \
  -d '{"text":"check-trace-propagation"}')
code=$(echo "$resp" | tail -n 1)
body=$(echo "$resp" | sed '$d')
if [ "$code" != "200" ]; then
  echo "  FAIL: POST /api/message-mongo returned HTTP $code"
  echo "$body"
  exit 1
fi
echo "  OK: API accepted request (HTTP 200)"

# Parse trace_id from API response (e.g. {"status":"ok","trace_id":"abc123...","endpoint":"MongoDB"})
TRACE_ID=$(echo "$body" | sed -n 's/.*"trace_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
if [ -z "$TRACE_ID" ]; then
  echo "  FAIL: No trace_id in API response: $body"
  exit 1
fi
echo "  trace_id from API: $TRACE_ID"

# 2. Wait for OTLP flush to Tempo
echo "  Waiting ${WAIT_FLUSH}s for trace flush..."
sleep "$WAIT_FLUSH"

# 3. Query Tempo for this trace (JSON). Use time range around now.
now=$(date +%s)
start=$((now - 120))
end=$((now + 10))
url="${TEMPO_URL}/api/traces/${TRACE_ID}?start=${start}&end=${end}"
trace_json=$(curl -s -H "Accept: application/json" "$url") || true

if [ -z "$trace_json" ]; then
  echo "  FAIL: Tempo returned empty response for trace $TRACE_ID"
  exit 1
fi

# Tempo may return trace ID as base64 in JSON; treat non-empty trace-shaped response as success
if echo "$trace_json" | grep -qE '"traceId"|"spans"|"scopeSpans"|"batches"'; then
  echo "  OK: Tempo returned trace for trace_id $TRACE_ID"
elif echo "$trace_json" | grep -q "$TRACE_ID"; then
  echo "  OK: Tempo returned trace containing trace_id $TRACE_ID"
else
  echo "  FAIL: Tempo response does not look like a trace for $TRACE_ID"
  echo "  Response (first 500 chars):"
  echo "$trace_json" | head -c 500
  echo ""
  exit 1
fi

# If jq is available, report span count (Tempo uses .batches[].scopeSpans[].spans[])
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
    echo "  OK: Trace has $span_count span(s) (propagation verified)"
  else
    echo "  WARN: Could not count spans (jq path may vary); trace presence accepted."
  fi
fi

echo "=== Trace propagation check: done ==="
