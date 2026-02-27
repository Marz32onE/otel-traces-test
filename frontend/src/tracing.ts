import { WebTracerProvider } from '@opentelemetry/sdk-trace-web'
import { SimpleSpanProcessor } from '@opentelemetry/sdk-trace-base'
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http'
import { Resource } from '@opentelemetry/resources'
import { W3CTraceContextPropagator } from '@opentelemetry/core'
import { trace } from '@opentelemetry/api'

const OTEL_COLLECTOR_URL =
  import.meta.env.VITE_OTEL_COLLECTOR_URL || 'http://localhost:4318'

const exporter = new OTLPTraceExporter({
  url: `${OTEL_COLLECTOR_URL}/v1/traces`,
})

const provider = new WebTracerProvider({
  resource: new Resource({ 'service.name': 'frontend' }),
})

provider.addSpanProcessor(new SimpleSpanProcessor(exporter))
provider.register({ propagator: new W3CTraceContextPropagator() })

export const tracer = trace.getTracer('frontend')
