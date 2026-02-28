import { WebTracerProvider } from '@opentelemetry/sdk-trace-web'
import { SimpleSpanProcessor, type ReadableSpan, type SpanExporter } from '@opentelemetry/sdk-trace-base'
import { resourceFromAttributes } from '@opentelemetry/resources'
import { W3CTraceContextPropagator, ExportResultCode, type ExportResult, hrTimeToNanoseconds } from '@opentelemetry/core'
import { trace } from '@opentelemetry/api'

const OTEL_COLLECTOR_URL =
  import.meta.env.VITE_OTEL_COLLECTOR_URL || 'http://localhost:4318'

function toHex(bytes: number[] | Uint8Array | string): string {
  if (typeof bytes === 'string') return bytes
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

function spansToOtlpJson(spans: ReadableSpan[]) {
  const resource = spans[0]?.resource
  const resAttrs = resource
    ? Object.entries(resource.attributes).map(([key, value]) => ({
        key,
        value: { stringValue: String(value) },
      }))
    : []

  return {
    resourceSpans: [
      {
        resource: { attributes: resAttrs },
        scopeSpans: [
          {
            scope: { name: 'frontend' },
            spans: spans.map((s) => ({
              traceId: toHex(s.spanContext().traceId),
              spanId: toHex(s.spanContext().spanId),
              parentSpanId: s.parentSpanId ? toHex(s.parentSpanId) : undefined,
              name: s.name,
              kind: s.kind + 1,
              startTimeUnixNano: String(hrTimeToNanoseconds(s.startTime)),
              endTimeUnixNano: String(hrTimeToNanoseconds(s.endTime)),
              attributes: Object.entries(s.attributes).map(([key, value]) => ({
                key,
                value: { stringValue: String(value) },
              })),
              status: { code: s.status.code },
            })),
          },
        ],
      },
    ],
  }
}

const fetchExporter: SpanExporter = {
  export(spans: ReadableSpan[], resultCallback: (result: ExportResult) => void) {
    const body = JSON.stringify(spansToOtlpJson(spans))
    fetch(`${OTEL_COLLECTOR_URL}/v1/traces`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body,
      keepalive: true,
    })
      .then((res) => {
        resultCallback({
          code: res.ok ? ExportResultCode.SUCCESS : ExportResultCode.FAILED,
        })
      })
      .catch(() => {
        resultCallback({ code: ExportResultCode.FAILED })
      })
  },
  shutdown() {
    return Promise.resolve()
  },
}

const provider = new WebTracerProvider({
  resource: resourceFromAttributes({ 'service.name': 'frontend' }),
  spanProcessors: [new SimpleSpanProcessor(fetchExporter)],
})

provider.register({ propagator: new W3CTraceContextPropagator() })

export const tracer = trace.getTracer('frontend')
