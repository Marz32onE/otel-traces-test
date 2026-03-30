import {
  DiagConsoleLogger,
  DiagLogLevel,
  diag,
  propagation,
  trace,
  type TextMapPropagator,
} from '@opentelemetry/api';
import {
  CompositePropagator,
  W3CBaggagePropagator,
  W3CTraceContextPropagator,
} from '@opentelemetry/core';
import { Resource } from '@opentelemetry/resources';
import { NodeTracerProvider } from '@opentelemetry/sdk-trace-node';
import { SimpleSpanProcessor } from '@opentelemetry/sdk-trace-base';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';

const _diagLevelMap: Record<string, DiagLogLevel> = {
  verbose: DiagLogLevel.VERBOSE,
  debug: DiagLogLevel.DEBUG,
  info: DiagLogLevel.INFO,
  warn: DiagLogLevel.WARN,
  error: DiagLogLevel.ERROR,
};

export function initOtel() {
  const logLevel = process.env.OTEL_LOG_LEVEL?.toLowerCase();
  if (logLevel && logLevel in _diagLevelMap) {
    diag.setLogger(new DiagConsoleLogger(), _diagLevelMap[logLevel]);
  }

  const otlpEndpoint =
    process.env.OTEL_EXPORTER_OTLP_ENDPOINT ?? 'http://localhost:4318';
  const tracesUrl = `${otlpEndpoint.replace(/\/$/, '')}/v1/traces`;

  const propagator: TextMapPropagator = new CompositePropagator({
    propagators: [new W3CTraceContextPropagator(), new W3CBaggagePropagator()],
  });

  // IMPORTANT: `@marz32one/otel-ws` (client + instrumentSocket server patch) uses
  // the global API tracer provider + global propagator, so we must register them here.
  propagation.setGlobalPropagator(propagator);

  const exporter = new OTLPTraceExporter({ url: tracesUrl });
  const provider = new NodeTracerProvider({
    resource: new Resource({
      'service.name': 'ws-node-backend',
      'service.version': process.env.SERVICE_VERSION ?? '0.0.1',
    }),
    spanProcessors: [new SimpleSpanProcessor(exporter)],
  });

  provider.register({ propagator });

  return {
    provider,
    shutdown: async () => {
      // Ensure exporter flushes before exit.
      try {
        await provider.shutdown();
      } finally {
        propagation.disable?.();
        trace.disable?.();
      }
    },
  };
}

