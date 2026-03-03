import { trace } from '@opentelemetry/api';
import { initializeFaro, getWebInstrumentations } from '@grafana/faro-react';
import { TracingInstrumentation } from '@grafana/faro-web-tracing';
import { OtlpHttpTransport } from '@grafana/faro-transport-otlp-http';

const OTEL_COLLECTOR_URL =
  import.meta.env.VITE_OTEL_COLLECTOR_URL || 'http://localhost:4318';
const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8081';

// Allow trace context (traceparent) to be sent to our API (cross-origin when port differs)
function apiOriginRegex(): RegExp {
  try {
    const u = new URL(API_URL);
    const origin = u.origin.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    return new RegExp(`^${origin}`);
  } catch {
    return /localhost:8081/;
  }
}

initializeFaro({
  url: OTEL_COLLECTOR_URL,
  app: {
    name: 'frontend',
    version: '0.0.1',
  },
  instrumentations: [
    ...getWebInstrumentations(),
    new TracingInstrumentation({
      instrumentationOptions: {
        propagateTraceHeaderCorsUrls: [apiOriginRegex()],
      },
    }),
  ],
  transports: [
    new OtlpHttpTransport({
      tracesURL: `${OTEL_COLLECTOR_URL}/v1/traces`,
    }),
  ],
});

export const tracer = trace.getTracer('frontend');
