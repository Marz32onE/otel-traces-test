---
name: debug-browser-traces
description: Systematic debugging workflow for browser-to-backend distributed tracing issues, especially "root span not yet received" in Grafana/Tempo. Use when browser OTel spans are missing, traces are incomplete, or rootServiceName shows unexpected values.
---

# Debug Browser Traces

Systematic process for diagnosing and fixing browser-originated OpenTelemetry trace issues. Follow each phase in order — do NOT skip ahead.

## Phase 1: Confirm the Symptom

```bash
# List recent traces and check rootServiceName
curl -s "http://localhost:3200/api/search?q={}&limit=10" | python3 -c "
import json, sys
for t in json.load(sys.stdin).get('traces', []):
    root = t['rootServiceName']
    ok = 'PASS' if root != '<root span not yet received>' else 'FAIL'
    svcs = ', '.join(f'{k}:{v[\"spanCount\"]}' for k,v in t.get('serviceStats',{}).items())
    print(f'  [{ok}] root={root} [{svcs}] id={t[\"traceID\"][:24]}')
"
```

If all traces show PASS, there is no issue. If FAIL traces only have `api:N` spans (no `frontend`), proceed to Phase 2.

## Phase 2: Isolate — Backend vs Frontend

Test the backend pipeline in isolation to rule it out:

```bash
TRACE_ID="deadbeef00000000000000000000ff01"
SPAN_ID="fe00000000000001"
NOW=$(python3 -c "import time; print(int(time.time() * 1e9))")

# Send API request with traceparent
curl -s http://localhost:8081/api/message -X POST \
  -H "Content-Type: application/json" \
  -H "traceparent: 00-${TRACE_ID}-${SPAN_ID}-01" \
  -d '{"text":"debug-test"}'

# Send matching frontend root span via OTLP HTTP
sleep 1
END=$(python3 -c "import time; print(int(time.time() * 1e9))")
curl -s http://localhost:4318/v1/traces -X POST \
  -H "Content-Type: application/json" \
  -d "{\"resourceSpans\":[{\"resource\":{\"attributes\":[{\"key\":\"service.name\",\"value\":{\"stringValue\":\"frontend\"}}]},\"scopeSpans\":[{\"scope\":{\"name\":\"frontend\"},\"spans\":[{\"traceId\":\"${TRACE_ID}\",\"spanId\":\"${SPAN_ID}\",\"name\":\"debug-root\",\"kind\":4,\"startTimeUnixNano\":\"${NOW}\",\"endTimeUnixNano\":\"${END}\",\"status\":{\"code\":1}}]}]}]}"

sleep 5
curl -s "http://localhost:3200/api/traces/${TRACE_ID}" | python3 -c "
import json, sys
data = json.load(sys.stdin)
for batch in data.get('batches', []):
    svc = next((a['value']['stringValue'] for a in batch['resource']['attributes'] if a['key'] == 'service.name'), '?')
    for ss in batch.get('scopeSpans', []):
        for span in ss.get('spans', []):
            pid = span.get('parentSpanId','')
            print(f'  [{svc}] {span[\"name\"]}  parent={pid[:16] if pid else \"ROOT\"}')
"
```

- **If this shows `[frontend]` + `[api]` spans**: backend is fine, problem is browser export → go to Phase 3
- **If `[frontend]` span missing**: OTel Collector or Tempo is broken → check `docker compose logs otel-collector tempo`

## Phase 3: Enable Collector Debug Logging

Add `debug` exporter to `otel-collector-config.yaml`:

```yaml
exporters:
  otlp_grpc/tempo:
    endpoint: "tempo:4317"
    tls:
      insecure: true
  debug:
    verbosity: detailed

service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [otlp_grpc/tempo, debug]
```

Then restart: `docker compose restart otel-collector api`

Now ask the user to send a message from the browser, then check:

```bash
docker compose logs otel-collector --since 2m 2>&1 | grep -E "service.name|Trace ID|Parent ID|Name "
```

Expected: both `service.name: Str(frontend)` and `service.name: Str(api)` entries with the same Trace ID.

## Phase 4: Check the JS Bundle

Verify what transport the frontend is actually using:

```bash
docker compose exec frontend sh -c "cat /usr/share/nginx/html/assets/*.js" 2>&1 \
  | grep -o "sendBeacon\|XMLHttpRequest\|FetchTransport\|keepalive\|fetch(" \
  | sort | uniq -c | sort -rn
```

| Finding | Meaning |
|---|---|
| `fetch(` + `keepalive` only | Custom fetch exporter — correct |
| `sendBeacon` or `XMLHttpRequest` present | Official OTel exporter — known to silently fail in Vite+browser |
| None of the above | Tracing code not bundled — check imports |

If `sendBeacon`/`XMLHttpRequest` found, the official `@opentelemetry/exporter-trace-otlp-http` is being used and must be replaced with the custom fetch exporter (see Phase 6).

## Phase 5: Add Browser Console Logging

Add `console.log` to the exporter's `export()` method so the user can check their browser DevTools Console:

```typescript
console.log('[OTel] exporting', spans.length, 'span(s) to', url,
  'traceId:', spans[0]?.spanContext().traceId)
// ... after fetch .then():
console.log('[OTel] export response:', res.status, res.statusText)
// ... in .catch():
console.error('[OTel] export FAILED:', err)
```

Rebuild frontend (`docker compose build --no-cache frontend && docker compose up -d frontend`), ask user to hard-refresh (Cmd+Shift+R) and check Console after sending a message.

| Console output | Next step |
|---|---|
| No `[OTel]` output at all | Tracing module not loaded — check `tracing.ts` import in `main.tsx` |
| `[OTel] exporting...` then nothing | `fetch()` hangs — check CORS (Phase 3) |
| `[OTel] export FAILED: TypeError` | Network error — check `localhost:4318` reachable |
| `[OTel] export response: 200 OK` | Export works — verify Trace ID matches API spans (Phase 7) |

## Phase 6: Fix — Custom Fetch Exporter

If the official `OTLPTraceExporter` silently fails, replace it with a custom `fetch()`-based exporter in `frontend/src/tracing.ts`:

1. Remove `@opentelemetry/exporter-trace-otlp-http` from `package.json`
2. Implement `SpanExporter` interface using native `fetch()` with `keepalive: true`
3. Manually serialize spans to OTLP JSON format
4. Use `SimpleSpanProcessor` (not `BatchSpanProcessor` — batch may delay or drop spans on page unload)

Key requirements:
- Use `fetch()` with `keepalive: true` (survives page navigation)
- Serialize to OTLP JSON (`/v1/traces` endpoint, not protobuf)
- Map `span.kind` from SDK enum (0-based) to OTLP enum (1-based): `s.kind + 1`
- Convert `hrTime` to nanosecond strings via `hrTimeToNanoseconds()`

## Phase 7: Verify Trace ID Correlation

After confirming export succeeds, verify the frontend span's trace ID matches the API spans:

```bash
# Get trace IDs from collector debug log
docker compose logs otel-collector --since 2m 2>&1 | grep -A 3 "Trace ID"
```

Both frontend and API spans must share the same Trace ID. The API's `Parent ID` must equal the frontend span's `ID`.

If IDs mismatch: `propagation.inject(ctx, headers)` in `App.tsx` is not working — check that the span is set in context before injection.

## Phase 8: Verify in Tempo

```bash
sleep 8  # Tempo ingestion delay
curl -s "http://localhost:3200/api/search?q={}&limit=5" | python3 -c "
import json, sys
for t in json.load(sys.stdin).get('traces', []):
    root = t['rootServiceName']
    ok = 'PASS' if root != '<root span not yet received>' else 'FAIL'
    svcs = ', '.join(f'{k}:{v[\"spanCount\"]}' for k,v in t.get('serviceStats',{}).items())
    print(f'  [{ok}] root={root} [{svcs}]')
"
```

Only mark as resolved when the user's browser-generated trace shows `root=frontend` with `frontend:1, api:2`.

## Phase 9: Cleanup

After issue is resolved:

1. Remove `console.log` from the exporter (or keep for development)
2. Remove `debug` exporter from `otel-collector-config.yaml`
3. Restart: `docker compose restart otel-collector api`

## Common Pitfalls

| Pitfall | Symptom | Fix |
|---|---|---|
| Docker build cache | Old JS bundle served after code change | `docker compose build --no-cache frontend` |
| Browser cache | Old JS loaded despite rebuild | Hard refresh: Cmd+Shift+R |
| Tempo ingestion delay | Trace query returns 404 right after send | Wait 5-8 seconds before querying |
| Collector restart breaks API gRPC | API logs `no route to host` | `docker compose restart otel-collector api` together |
| OTLP trace ID must be hex | Collector rejects non-hex IDs | Use only 0-9 a-f, exactly 32 chars |
| curl test != browser test | curl works but browser doesn't | Always verify with real browser after curl passes |
