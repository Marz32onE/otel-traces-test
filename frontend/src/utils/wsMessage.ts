import { context } from "@opentelemetry/api";
import type { Context } from "@opentelemetry/api";
import {
  extractMessageContext,
  parseIncomingMessage,
} from "@marz32one/otel-websocket/browser";

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
  const parsed = parseIncomingMessage(data);
  const ctx = extractMessageContext(parsed, context.active());
  return {
    displayText: parsed.displayText,
    traceId: parsed.traceId,
    context: ctx,
  };
}
