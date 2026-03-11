import { context, propagation } from "@opentelemetry/api";
import type { Context } from "@opentelemetry/api";
import { traceIdFromTraceparent } from "./trace";

export type WsParsedMessage = {
  displayText: string;
  traceId: string | null;
  context: Context;
};

/**
 * Parse WebSocket message: otelwebsocket envelope (headers + base64 payload)
 * or legacy { traceparent, tracestate, body, api }. Returns display text,
 * trace ID for UI, and extracted context for span creation.
 */
export function parseWsMessage(data: string): WsParsedMessage {
  let body = data;
  let ctx = context.active();
  let displayText = body;

  try {
    const parsed = JSON.parse(data) as Record<string, unknown>;

    if (parsed.headers && typeof parsed.payload === "string") {
      const headers = parsed.headers as Record<string, string>;
      let payloadStr: string;
      try {
        payloadStr = atob(parsed.payload);
      } catch {
        payloadStr = parsed.payload;
      }
      let appPayload: { body?: string; api?: string } = {};
      try {
        appPayload = JSON.parse(payloadStr) as { body?: string; api?: string };
      } catch {
        appPayload = { body: payloadStr };
      }
      body = appPayload.body ?? payloadStr;
      displayText = appPayload.api ? `${body} [${appPayload.api}]` : body;
      const tid = headers.traceparent
        ? traceIdFromTraceparent(headers.traceparent)
        : null;
      const carrier: Record<string, string> = {};
      if (headers.traceparent) carrier.traceparent = headers.traceparent;
      if (headers.tracestate) carrier.tracestate = headers.tracestate;
      if (Object.keys(carrier).length) ctx = propagation.extract(ctx, carrier);
      return { displayText, traceId: tid, context: ctx };
    }

    if (
      typeof parsed.traceparent === "string" &&
      parsed.body !== undefined
    ) {
      body = String(parsed.body);
      displayText = parsed.api ? `${body} [${parsed.api}]` : body;
      const tid = traceIdFromTraceparent(parsed.traceparent);
      const carrier: Record<string, string> = {};
      if (parsed.traceparent) carrier.traceparent = parsed.traceparent;
      if (parsed.tracestate)
        carrier.tracestate = String(parsed.tracestate ?? "");
      ctx = propagation.extract(ctx, carrier);
      return { displayText, traceId: tid, context: ctx };
    }
  } catch {
    /* plain text */
  }

  return { displayText, traceId: null, context: ctx };
}
