import { useCallback, useEffect, useState } from "react";
import {
  context,
  trace,
  SpanKind,
  SpanStatusCode,
} from "@opentelemetry/api";
import { wsconnect, type OtelNatsConn } from "@marz32one/otel-nats/browser";
import { createJetStream } from "@marz32one/otel-nats/jetstream";
import { tracer } from "../tracing";
import { NATS_WS_URL } from "../constants/env";

const textEncoder = new TextEncoder();

/**
 * Browser NATS over WebSocket (@marz32one/otel-nats/browser), mirroring API subjects
 * {@code messages.new} (JetStream) and {@code messages.core} (core).
 */
export function useNatsBrowser() {
  const [natsConn, setNatsConn] = useState<OtelNatsConn | null>(null);
  const [natsWsStatus, setNatsWsStatus] = useState("Connecting…");

  useEffect(() => {
    let cancelled = false;
    let opened: OtelNatsConn | null = null;

    void (async () => {
      try {
        const c = await wsconnect({ servers: NATS_WS_URL });
        if (cancelled) {
          await c.close();
          return;
        }
        opened = c;
        setNatsConn(c);
        setNatsWsStatus(`Connected (${NATS_WS_URL})`);
      } catch (err) {
        if (!cancelled) {
          const msg = err instanceof Error ? err.message : String(err);
          setNatsWsStatus(`Error: ${msg}`);
        }
      }
    })();

    return () => {
      cancelled = true;
      const c = opened;
      if (c) {
        void c
          .drain()
          .catch(() => undefined)
          .finally(() => {
            void c.close();
          });
      }
    };
  }, []);

  const publishJetStream = useCallback(async (text: string) => {
    const trimmed = text.trim();
    if (!natsConn || !trimmed) return;

    const span = tracer.startSpan("send-message-jetstream-browser", {
      kind: SpanKind.CLIENT,
      attributes: {
        "message.content": trimmed,
        "messaging.system": "nats",
      },
    });
    const ctx = trace.setSpan(context.active(), span);

    try {
      await context.with(ctx, async () => {
        const js = createJetStream(natsConn);
        await js.publish("messages.new", textEncoder.encode(trimmed));
      });
      span.setStatus({ code: SpanStatusCode.OK });
    } catch (err) {
      const error = err instanceof Error ? err : new Error(String(err));
      span.setStatus({ code: SpanStatusCode.ERROR, message: error.message });
      span.recordException(error);
      alert(`NATS JetStream: ${error.message}`);
      throw error;
    } finally {
      span.end();
    }
  }, [natsConn]);

  const publishCore = useCallback(
    (text: string) => {
      const trimmed = text.trim();
      if (!natsConn || !trimmed) return;

      const span = tracer.startSpan("send-message-core-browser", {
        kind: SpanKind.CLIENT,
        attributes: {
          "message.content": trimmed,
          "messaging.system": "nats",
        },
      });
      const ctx = trace.setSpan(context.active(), span);

      try {
        context.with(ctx, () => {
          natsConn.publish("messages.core", trimmed);
        });
        span.setStatus({ code: SpanStatusCode.OK });
      } catch (err) {
        const error = err instanceof Error ? err : new Error(String(err));
        span.setStatus({ code: SpanStatusCode.ERROR, message: error.message });
        span.recordException(error);
        alert(`NATS Core: ${error.message}`);
        throw error;
      } finally {
        span.end();
      }
    },
    [natsConn],
  );

  return {
    natsWsStatus,
    natsReady: natsConn !== null,
    publishJetStream,
    publishCore,
  };
}
