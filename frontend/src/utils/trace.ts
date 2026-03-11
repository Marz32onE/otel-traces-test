/**
 * Parse trace ID from W3C traceparent (version-traceId-spanId-flags).
 */
export function traceIdFromTraceparent(traceparent: string): string | null {
  const parts = traceparent.trim().split("-");
  return parts.length >= 2 ? (parts[1] ?? null) : null;
}
