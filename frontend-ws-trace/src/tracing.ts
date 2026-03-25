import { trace } from '@opentelemetry/api';
import { initializeFaro, getWebInstrumentations } from '@grafana/faro-react';
import { TracingInstrumentation } from '@grafana/faro-web-tracing';
import { OtlpHttpTransport } from '@grafana/faro-transport-otlp-http';

const OTEL_COLLECTOR_URL =
  import.meta.env.VITE_OTEL_COLLECTOR_URL ?? 'http://localhost:4318';

initializeFaro({
  url: OTEL_COLLECTOR_URL,
  app: {
    name: 'frontend-ws-trace',
    version: '0.0.1',
  },
  instrumentations: [
    ...getWebInstrumentations(),
    new TracingInstrumentation({
      // No API in this lite UI, but keep the propagation config permissive.
      instrumentationOptions: {
        propagateTraceHeaderCorsUrls: [/^http:\/\/localhost:\d+$/],
      },
    }),
  ],
  transports: [
    new OtlpHttpTransport({
      tracesURL: `${OTEL_COLLECTOR_URL}/v1/traces`,
    }),
  ],
});

export const tracer = trace.getTracer('frontend-ws-trace');

