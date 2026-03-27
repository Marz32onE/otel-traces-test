import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  context,
  trace,
  SpanKind,
  SpanStatusCode,
} from '@opentelemetry/api';
import { webSocket } from '@marz32one/otel-rxjs-ws';
import type { WebSocketSubject } from 'rxjs/webSocket';

import { tracer } from './tracing';

type ServerAck = { ack: true; echo: string; traceId?: string };

const WS_OTEL_URL_DEFAULT = 'ws://localhost:8085/otel-ws';
const WS_PLAIN_URL_DEFAULT = 'ws://localhost:8085/ws';
const RECONNECT_MS = 2000;

function appendLine(setter: (updater: (prev: string[]) => string[]) => void, text: string) {
  setter((prev) => [...prev.slice(-59), text]);
}

function asText(value: unknown): string {
  if (typeof value === 'string') return value;
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function defaultWsUrl(path: '/otel-ws' | '/ws'): string {
  if (typeof window === 'undefined') {
    return path === '/otel-ws' ? WS_OTEL_URL_DEFAULT : WS_PLAIN_URL_DEFAULT;
  }
  const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
  return `${protocol}://${window.location.host}${path}`;
}

export default function App() {
  const wsOtelUrl = useMemo(
    () => import.meta.env.VITE_WS_OTEL_URL ?? defaultWsUrl('/otel-ws'),
    [],
  );
  const wsPlainUrl = useMemo(
    () => import.meta.env.VITE_WS_PLAIN_URL ?? defaultWsUrl('/ws'),
    [],
  );

  const [inputText, setInputText] = useState('{"text":"check-ws-trace"}');
  const [clientTraceId, setClientTraceId] = useState<string | null>(null);

  const [otelStatus, setOtelStatus] = useState('Connecting...');
  const [plainStatus, setPlainStatus] = useState('Connecting...');
  const [otelTraceId, setOtelTraceId] = useState<string | null>(null);

  const [clientLog, setClientLog] = useState<string[]>([]);
  const [otelLog, setOtelLog] = useState<string[]>([]);
  const [plainLog, setPlainLog] = useState<string[]>([]);
  const [otelRetry, setOtelRetry] = useState(0);
  const [plainRetry, setPlainRetry] = useState(0);

  const otelSocketRef = useRef<WebSocketSubject<unknown> | null>(null);
  const plainSocketRef = useRef<WebSocket | null>(null);
  const otelRetryTimerRef = useRef<number | null>(null);
  const plainRetryTimerRef = useRef<number | null>(null);

  const parsePayload = useCallback((): unknown => {
    const value = inputText.trim();
    if (!value) return '';
    try {
      return JSON.parse(value) as unknown;
    } catch {
      return value;
    }
  }, [inputText]);

  useEffect(() => {
    const wsOtel = webSocket<unknown>({
      url: wsOtelUrl,
      openObserver: {
        next: () => setOtelStatus('Connected'),
      },
      closeObserver: {
        next: () => {
          setOtelStatus('Reconnecting...');
          if (otelRetryTimerRef.current) window.clearTimeout(otelRetryTimerRef.current);
          otelRetryTimerRef.current = window.setTimeout(() => {
            setOtelRetry((v) => v + 1);
          }, RECONNECT_MS);
        },
      },
    });
    otelSocketRef.current = wsOtel;

    const otelSub = wsOtel.subscribe({
      next: (msg) => {
        const ack = msg as ServerAck;
        appendLine(setOtelLog, asText(ack));
        if (ack.traceId) setOtelTraceId(ack.traceId);
      },
      error: (err) => {
        setOtelStatus('Reconnecting...');
        appendLine(setOtelLog, `error: ${asText(err)}`);
        if (otelRetryTimerRef.current) window.clearTimeout(otelRetryTimerRef.current);
        otelRetryTimerRef.current = window.setTimeout(() => {
          setOtelRetry((v) => v + 1);
        }, RECONNECT_MS);
      },
      complete: () => {
        setOtelStatus('Reconnecting...');
        if (otelRetryTimerRef.current) window.clearTimeout(otelRetryTimerRef.current);
        otelRetryTimerRef.current = window.setTimeout(() => {
          setOtelRetry((v) => v + 1);
        }, RECONNECT_MS);
      },
    });

    return () => {
      otelSub.unsubscribe();
      wsOtel.complete();
      if (otelRetryTimerRef.current) {
        window.clearTimeout(otelRetryTimerRef.current);
        otelRetryTimerRef.current = null;
      }
    };
  }, [wsOtelUrl, otelRetry]);

  useEffect(() => {
    const wsPlain = new WebSocket(wsPlainUrl);
    plainSocketRef.current = wsPlain;
    wsPlain.onopen = () => setPlainStatus('Connected');
    wsPlain.onclose = () => {
      setPlainStatus('Reconnecting...');
      if (plainRetryTimerRef.current) window.clearTimeout(plainRetryTimerRef.current);
      plainRetryTimerRef.current = window.setTimeout(() => {
        setPlainRetry((v) => v + 1);
      }, RECONNECT_MS);
    };
    wsPlain.onerror = () => {
      setPlainStatus('Reconnecting...');
    };
    wsPlain.onmessage = (event) => {
      appendLine(setPlainLog, String(event.data));
    };

    return () => {
      wsPlain.close();
      if (plainRetryTimerRef.current) {
        window.clearTimeout(plainRetryTimerRef.current);
        plainRetryTimerRef.current = null;
      }
    };
  }, [wsPlainUrl, plainRetry]);

  const handleSend = useCallback(() => {
    const payload = parsePayload();

    const parent = tracer.startSpan('frontend.verify.send', {
      kind: SpanKind.CLIENT,
      attributes: {
        'messaging.system': 'websocket',
        'messaging.operation': 'send',
      },
    });
    const parentCtx = trace.setSpan(context.active(), parent);
    context.with(parentCtx, () => {
      otelSocketRef.current?.next(payload);
    });
    setClientTraceId(parent.spanContext().traceId);
    appendLine(setClientLog, `send: ${asText(payload)}`);
    parent.setStatus({ code: SpanStatusCode.OK });
    parent.end();

    const plainSocket = plainSocketRef.current;
    if (plainSocket?.readyState === WebSocket.OPEN) {
      plainSocket.send(asText(payload));
    } else {
      appendLine(setPlainLog, 'error: plain ws not connected');
    }
  }, [parsePayload]);

  return (
    <div style={{ fontFamily: 'sans-serif', padding: 16, maxWidth: 1200 }}>
      <h1>WS Trace Verify UI</h1>

      <p>Input (string or JSON):</p>
      <textarea
        rows={5}
        style={{ width: '100%', marginBottom: 8 }}
        value={inputText}
        onChange={(e) => setInputText(e.target.value)}
      />
      <button type="button" onClick={handleSend}>
        Send To /otel-ws + /ws
      </button>

      <div
        style={{
          marginTop: 16,
          display: 'grid',
          gridTemplateColumns: 'repeat(3, minmax(0, 1fr))',
          gap: 12,
        }}
      >
        <section>
          <h3>otel-rxjs-ws (client)</h3>
          <p>Client Trace ID: <b>{clientTraceId ?? '-'}</b></p>
          <textarea readOnly rows={16} style={{ width: '100%' }} value={clientLog.join('\n')} />
        </section>

        <section>
          <h3>otel-ws endpoint (/otel-ws)</h3>
          <p>Status: <b>{otelStatus}</b></p>
          <p>Server Trace ID: <b>{otelTraceId ?? '-'}</b></p>
          <textarea readOnly rows={16} style={{ width: '100%' }} value={otelLog.join('\n')} />
        </section>

        <section>
          <h3>plain ws endpoint (/ws)</h3>
          <p>Status: <b>{plainStatus}</b></p>
          <p>Trace ID: <b>-</b></p>
          <textarea readOnly rows={16} style={{ width: '100%' }} value={plainLog.join('\n')} />
        </section>
      </div>
    </div>
  );
}

