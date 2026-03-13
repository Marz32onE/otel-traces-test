import { trace } from '@opentelemetry/api';
import { initializeFaro, getWebInstrumentations } from '@grafana/faro-react';
import { TracingInstrumentation } from '@grafana/faro-web-tracing';
import { OtlpHttpTransport } from '@grafana/faro-transport-otlp-http';
import { API_URL, API_V1_URL } from './constants/env';

const OTEL_COLLECTOR_URL =
  import.meta.env.VITE_OTEL_COLLECTOR_URL || 'http://localhost:4318';

// Allow trace context (traceparent) to be sent to our API (cross-origin when port differs)
function originToRegex(url: string): RegExp {
  try {
    const u = new URL(url);
    const origin = u.origin.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    return new RegExp(`^${origin}`);
  } catch {
    return /^http:\/\/localhost:\d+$/;
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
        // Both API (v2) and API v1 (mongo v1) so trace propagates to each
        propagateTraceHeaderCorsUrls: [originToRegex(API_URL), originToRegex(API_V1_URL)],
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
