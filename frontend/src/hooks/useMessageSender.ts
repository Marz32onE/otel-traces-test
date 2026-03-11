import { useState, useCallback } from "react";
import { context, trace, SpanStatusCode, SpanKind } from "@opentelemetry/api";
import { tracer } from "../tracing";
import { API_URL } from "../constants/env";
import { DEFAULT_MONGO_ID, ENDPOINTS_NEED_TEXT } from "../constants/endpoints";
import type { LastTrace, SendPayload, ApiResponse, SendToEndpointOptions } from "../types";

export function useMessageSender() {
  const [lastTrace, setLastTrace] = useState<LastTrace>(null);
  const [lastMongoId, setLastMongoId] = useState<string | null>(null);
  const [mongoId, setMongoId] = useState(DEFAULT_MONGO_ID);

  const sendToEndpoint = useCallback(
    async (
      endpoint: string,
      spanName: string,
      body?: SendPayload,
      options?: SendToEndpointOptions,
    ) => {
      const payload = body ?? {};
      if (!("id" in payload) && !("text" in payload)) return;
      const needsText = (ENDPOINTS_NEED_TEXT as readonly string[]).includes(endpoint);
      if (needsText && (!payload.text || !payload.text.trim())) return;

      const url = `${API_URL}${endpoint}`;
      const span = tracer.startSpan(spanName, {
        kind: SpanKind.CLIENT,
        attributes: {
          "message.content": "text" in payload ? (payload.text ?? "") : "",
          "http.request.method": "POST",
          "url.full": url,
        },
      });
      const ctx = trace.setSpan(context.active(), span);

      try {
        const res = await context.with(ctx, () =>
          fetch(url, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(payload),
          }),
        );
        span.setAttribute("http.response.status_code", res.status);
        const data = (await res.json()) as ApiResponse;
        if (!res.ok) throw new Error(data.error ?? "Request failed");

        span.setStatus({ code: SpanStatusCode.OK });
        options?.onSuccess?.();

        if (data.trace_id) {
          setLastTrace({
            traceId: data.trace_id,
            endpoint: data.endpoint ?? endpoint,
            id: data.id,
          });
          if (data.id) {
            setLastMongoId(data.id);
            setMongoId(data.id);
          }
          if (data.endpoint === "MongoDB Delete") setLastMongoId(null);
        }
      } catch (err) {
        const error = err instanceof Error ? err : new Error(String(err));
        span.setStatus({ code: SpanStatusCode.ERROR, message: error.message });
        span.recordException(error);
        alert(`Error: ${error.message}`);
      } finally {
        span.end();
      }
    },
    [],
  );

  return { sendToEndpoint, lastTrace, lastMongoId, mongoId, setMongoId };
}
