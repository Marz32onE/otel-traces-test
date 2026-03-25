import { useEffect, useMemo, useState } from 'react';
import {
  context,
  trace,
  SpanKind,
  SpanStatusCode,
} from '@opentelemetry/api';
import { webSocket } from '@marz32one/otel-rxjs-ws';
import type { Subscription } from 'rxjs';
import { Subject, EMPTY, timer } from 'rxjs';
import { catchError, switchMap, startWith, tap } from 'rxjs/operators';

import { tracer } from './tracing';

type ClientMessage = { text: string };
type ServerAck = { ack: true; echo: string; traceId?: string };

const WS_URL_DEFAULT = 'ws://localhost:8085/ws';

export default function App() {
  const WS_URL = useMemo(
    () => import.meta.env.VITE_WS_URL ?? WS_URL_DEFAULT,
    [],
  );

  const [status, setStatus] = useState('Connecting...');
  const [lastTraceId, setLastTraceId] = useState<string | null>(null);
  const [echo, setEcho] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    let sub: Subscription | null = null;
    const reconnectSignal$ = new Subject<void>();
    const RECONNECT_MS = 3000;

    sub = reconnectSignal$
      .pipe(
        startWith(void 0),
        switchMap(() => {
          setStatus('Connecting...');

          const payload: ClientMessage = { text: 'check-ws-trace' };

          let wsSubject: ReturnType<typeof webSocket<any>>;
          wsSubject = webSocket<any>({
            url: WS_URL,
            openObserver: {
              next: () => {
                if (cancelled) return;
                setStatus('Connected');

                const parent = tracer.startSpan(
                  'frontend.websocket.handshake',
                  {
                    kind: SpanKind.CLIENT,
                    attributes: {
                      'messaging.system': 'websocket',
                      'messaging.operation': 'send',
                    },
                  },
                );

                const parentCtx = trace.setSpan(context.active(), parent);
                context.with(parentCtx, () => {
                  wsSubject.next(payload);
                });

                setLastTraceId(parent.spanContext().traceId);
                parent.setStatus({ code: SpanStatusCode.OK });
                parent.end();
              },
            },
          });

          return wsSubject.pipe(
            tap({
              next: (msg) => {
                if (cancelled) return;
                setStatus('Received');
                const ack = msg as ServerAck;
                setEcho(ack.echo);
                if (ack.traceId) setLastTraceId(ack.traceId);
              },
              complete: () => {
                if (cancelled) return;
                setStatus('Reconnecting...');
                timer(RECONNECT_MS).subscribe(() =>
                  reconnectSignal$.next(),
                );
              },
            }),
            catchError(() => {
              if (cancelled) return EMPTY;
              setStatus('Error. Reconnecting...');
              timer(RECONNECT_MS).subscribe(() =>
                reconnectSignal$.next(),
              );
              return EMPTY;
            }),
          );
        }),
      )
      .subscribe();

    return () => {
      cancelled = true;
      sub?.unsubscribe();
      reconnectSignal$.complete();
    };
  }, [WS_URL]);

  return (
    <div style={{ fontFamily: 'sans-serif', padding: 16 }}>
      <h1>WS Trace Propagation (RxJS + Node ws)</h1>
      <p>
        Status: <b>{status}</b>
      </p>

      <p>
        Sent traceId: <b>{lastTraceId ?? '-'}</b>
      </p>

      <p>
        Server echo: <b>{echo ?? '-'}</b>
      </p>
    </div>
  );
}

