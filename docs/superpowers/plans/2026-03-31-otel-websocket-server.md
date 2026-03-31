# OtelWebSocketServer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `OtelWebSocketServer` to `@marz32one/otel-ws` so users can `new OtelWebSocketServer(options)` and get automatic per-connection instrumentation with no `instrumentSocket` call needed.

**Architecture:** Subclass `BaseWebSocket.Server`; register a `connection` listener in the constructor that calls the existing `instrumentSocket(ws)` on every accepted socket. EventEmitter FIFO ordering guarantees the socket is patched before any user-registered `connection` handler runs.

**Tech Stack:** TypeScript, `ws@5.1.1`, `@opentelemetry/api`, Jest + ts-jest (ESM)

---

## Files

| File | Change |
|------|--------|
| `pkg/instrumentation-js/packages/otel-ws/src/index.ts` | Add `OtelWebSocketServer` class; add named export |
| `pkg/instrumentation-js/packages/otel-ws/test/index.test.ts` | Add 2 tests for `OtelWebSocketServer` |

---

### Task 1: Add `OtelWebSocketServer` class and export

**Files:**
- Modify: `pkg/instrumentation-js/packages/otel-ws/src/index.ts`

- [ ] **Step 1: Write the failing tests first**

Add these two tests inside the existing `describe('otel-ws', () => { ... })` block at the end of
`pkg/instrumentation-js/packages/otel-ws/test/index.test.ts`, after the last existing `it(...)`:

```typescript
  it('OtelWebSocketServer auto-instruments server sockets on connection', async () => {
    const { OtelWebSocketServer } = await import('../src/index.js');
    const wss = new OtelWebSocketServer({ port: 0 });
    const receiveTraceId = await new Promise<string | undefined>((resolve, reject) => {
      wss.on('connection', (ws) => {
        // No instrumentSocket call — auto-instrumented by OtelWebSocketServer
        ws.on('message', () => {
          resolve(trace.getSpanContext(context.active())?.traceId);
        });
      });
      const port = (wss.address() as AddressInfo).port;
      const client = new WsPkg(`ws://127.0.0.1:${port}`);
      client.once('open', () => {
        client.send(
          JSON.stringify({
            header: { traceparent: '00-eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee-ffffffffffffffff-01' },
            data: { text: 'auto-instrumented' },
          }),
          (err) => { if (err) reject(err); },
        );
      });
      client.once('error', reject);
    });

    expect(receiveTraceId).toBe('eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee');
    const spans = exporter.getFinishedSpans();
    expect(spans.some((s) => s.name === 'websocket.receive')).toBeTruthy();

    await new Promise<void>((r) => wss.close(() => r()));
  });

  it('OtelWebSocketServer sends with trace context injected', async () => {
    const { OtelWebSocketServer } = await import('../src/index.js');
    const wss = new OtelWebSocketServer({ port: 0 });
    wss.on('connection', (ws) => {
      // No instrumentSocket call — send is auto-patched
      ws.send({ reply: true });
    });
    const port = (wss.address() as AddressInfo).port;

    const wire = await new Promise<string>((resolve, reject) => {
      const client = new WsPkg(`ws://127.0.0.1:${port}`);
      client.on('message', (data) => resolve(data.toString()));
      client.once('error', reject);
    });

    const parsed = JSON.parse(wire) as Record<string, unknown>;
    expect(parsed.header).toBeDefined();
    expect((parsed.header as Record<string, unknown>).traceparent).toBeDefined();
    expect(parsed.data).toEqual({ reply: true });

    await new Promise<void>((r) => wss.close(() => r()));
  });
```

- [ ] **Step 2: Run the tests to confirm they fail**

```bash
cd pkg/instrumentation-js/packages/otel-ws
npm test -- --testNamePattern="OtelWebSocketServer"
```

Expected: both tests FAIL with `OtelWebSocketServer is not a constructor` (or similar import error).

- [ ] **Step 3: Add `OtelWebSocketServer` to `src/index.ts`**

Add this class right after the existing `OtelWebSocket` class (after line 49, before `export function instrumentSocket`):

```typescript
export class OtelWebSocketServer extends BaseWebSocket.Server {
  constructor(options?: BaseWebSocket.ServerOptions, callback?: () => void) {
    super(options, callback);
    this.on('connection', (ws: BaseWebSocket) => instrumentSocket(ws));
  }
}
```

- [ ] **Step 4: Run the new tests and confirm they pass**

```bash
cd pkg/instrumentation-js/packages/otel-ws
npm test -- --testNamePattern="OtelWebSocketServer"
```

Expected: both tests PASS.

- [ ] **Step 5: Run the full test suite to confirm no regressions**

```bash
cd pkg/instrumentation-js/packages/otel-ws
npm test
```

Expected: all tests PASS (10 total — 8 existing + 2 new).

- [ ] **Step 6: Run lint**

```bash
cd pkg/instrumentation-js/packages/otel-ws
npm run lint
```

Expected: no errors.

- [ ] **Step 7: Build**

```bash
cd pkg/instrumentation-js/packages/otel-ws
npm run build
```

Expected: exits 0, `dist/` updated.

- [ ] **Step 8: Commit**

```bash
cd pkg/instrumentation-js/packages/otel-ws
git add src/index.ts test/index.test.ts
git commit -m "feat(otel-ws): add OtelWebSocketServer for auto-instrumented server sockets"
```

---

## Verification

After the task completes, confirm end-to-end:

1. **Tests pass:** `npm test` in `pkg/instrumentation-js/packages/otel-ws` — 10 tests, 0 failures.
2. **Lint clean:** `npm run lint` exits 0.
3. **Build succeeds:** `dist/index.js` and `dist/index.d.ts` include `OtelWebSocketServer`.
4. **Type check the new export** — confirm `dist/index.d.ts` contains `export declare class OtelWebSocketServer extends WebSocket.Server`.
5. **Optional — use in ws-node-backend:** replace `new WebSocket.Server(...)` + `instrumentSocket(ws)` with `new OtelWebSocketServer(...)` and verify the existing ws-trace stack still works (`make up-ws-trace && make verify-ws-trace`).
