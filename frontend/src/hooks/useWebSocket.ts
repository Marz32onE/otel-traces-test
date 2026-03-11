import { useState, useEffect, useRef } from "react";
import { context, trace, SpanStatusCode, SpanKind } from "@opentelemetry/api";
import { tracer } from "../tracing";
import { WS_URL } from "../constants/env";
import { parseWsMessage } from "../utils/wsMessage";

export function useWebSocket() {
  const [messages, setMessages] = useState<string[]>([]);
  const [status, setStatus] = useState("Connecting...");
  const [lastReceivedTraceId, setLastReceivedTraceId] = useState<string | null>(
    null,
  );
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    function connect() {
      const ws = new WebSocket(`${WS_URL}/ws`);
      wsRef.current = ws;

      ws.onopen = () => setStatus("Connected");
      ws.onclose = () => {
        setStatus("Reconnecting...");
        reconnectRef.current = setTimeout(connect, 3000);
      };
      ws.onerror = () => setStatus("Error");
      ws.onmessage = (event: MessageEvent) => {
        const data = typeof event.data === "string" ? event.data : "";
        const { displayText, traceId, context: ctx } = parseWsMessage(data);
        if (traceId) setLastReceivedTraceId(traceId);
        context.with(ctx, () => {
          const span = tracer.startSpan("receive message", {
            kind: SpanKind.CONSUMER,
            attributes: {
              "message.content": data,
              "messaging.operation": "receive",
            },
          });
          span.setStatus({ code: SpanStatusCode.OK });
          span.end();
        });
        setMessages((prev) => [...prev, displayText]);
      };
    }

    connect();
    return () => {
      if (wsRef.current) wsRef.current.close();
      if (reconnectRef.current) clearTimeout(reconnectRef.current);
    };
  }, []);

  return { messages, status, lastReceivedTraceId };
}
