import { useState, useEffect, useRef } from "react";
import {
  context,
  trace,
  SpanStatusCode,
  SpanKind,
} from "@opentelemetry/api";
import type { Subscription } from "rxjs";
import { webSocket } from "@marz32one/otel-rxjs-ws";
import { tracer } from "../tracing";
import { WS_URL } from "../constants/env";

/** Worker `wsPayload` JSON inside the instrumented `data` field (Go worker). */
export type WorkerMessagePayload = {
  body: string;
  api?: string;
};

function formatDisplay(m: WorkerMessagePayload): string {
  return m.api ? `${m.body} [${m.api}]` : m.body;
}

export function useWebSocket() {
  const [messages, setMessages] = useState<string[]>([]);
  const [status, setStatus] = useState("Connecting...");
  const [lastReceivedTraceId, setLastReceivedTraceId] = useState<string | null>(
    null,
  );
  const subRef = useRef<Subscription | null>(null);
  const reconnectRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    let cancelled = false;

    function connect() {
      if (cancelled) return;

      const ws = webSocket<WorkerMessagePayload>({
        url: `${WS_URL}/ws`,
        openObserver: {
          next: () => setStatus("Connected"),
        },
      });

      subRef.current = ws.subscribe({
        next: (msg) => {
          const displayText = formatDisplay(msg);
          const tid = trace.getSpanContext(context.active())?.traceId;
          if (tid) setLastReceivedTraceId(tid);

          const span = tracer.startSpan("receive message", {
            kind: SpanKind.CONSUMER,
            attributes: {
              "message.content": displayText,
              "messaging.operation": "receive",
            },
          });
          span.setStatus({ code: SpanStatusCode.OK });
          span.end();

          setMessages((prev) => [...prev, displayText]);
        },
        error: () => {
          setStatus("Error");
          reconnectRef.current = setTimeout(connect, 3000);
        },
        complete: () => {
          if (cancelled) return;
          setStatus("Reconnecting...");
          reconnectRef.current = setTimeout(connect, 3000);
        },
      });
    }

    connect();

    return () => {
      cancelled = true;
      if (reconnectRef.current) clearTimeout(reconnectRef.current);
      subRef.current?.unsubscribe();
      subRef.current = null;
    };
  }, []);

  return { messages, status, lastReceivedTraceId };
}
