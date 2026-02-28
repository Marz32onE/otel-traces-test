---
name: golang-o11y
description: Instruments Go services with OpenTelemetry tracing, Prometheus metrics, and slog structured logging. Use when adding or debugging observability in Go microservices, setting up distributed tracing, metrics endpoints, or correlating logs with traces.
---

# Go Observability (OpenTelemetry + Prometheus + slog)

Observability in Go rests on three pillars: **traces** (request flow), **metrics** (counters/gauges/histograms), and **logs** (structured events). Correlate them with Trace ID, Span ID, and Request ID.

## When to Use This Skill

- Adding or fixing observability in Go microservices
- Setting up distributed tracing (OpenTelemetry) across services
- Exposing Prometheus metrics and /metrics endpoint
- Using slog for structured, trace-correlated logging
- Implementing health checks (liveness/readiness) and graceful shutdown
- Debugging production issues with traces and metrics

## Core Principles

- **Traces**: Request flow across services; use context propagation.
- **Metrics**: Counters, gauges, histograms; keep label cardinality bounded.
- **Logs**: slog (Go 1.21+) with trace_id/span_id for correlation.

All three should share Trace ID (and optionally Request ID) so logs and metrics can be tied to spans.

## Quick Start

1. **Tracer**: Initialize OTel tracer provider with OTLP or Jaeger exporter; set as global provider; defer `tp.Shutdown(ctx)`.
2. **Spans**: `ctx, span := otel.Tracer("svc").Start(ctx, "op"); defer span.End();` pass `ctx` through the call chain.
3. **HTTP**: Use `otelhttp.NewHandler(handler, "route-name")` for auto-instrumentation.
4. **Metrics**: Register Prometheus counters/histograms; expose `/metrics` with `promhttp.Handler()`.
5. **Logs**: Use `slog` with `slog.With("trace_id", traceID)`; include trace_id in log attributes.
6. **Health**: Implement `/health` (liveness) and `/ready` (readiness); call `tp.Shutdown` on SIGTERM.

## Quick Reference

```go
// Init tracer
tp, _ := initTracer("service-name")
defer tp.Shutdown(context.Background())

// Span
ctx, span := otel.Tracer("name").Start(ctx, "operation")
defer span.End()
span.SetAttributes(attribute.String("key", "value"))

// Metrics
counter := promauto.NewCounterVec(opts, []string{"label"})
histogram := promauto.NewHistogramVec(opts, []string{"label"})

// Logging
logger := slog.With("trace_id", traceID)
logger.Info("message", "key", value)

// Health
http.HandleFunc("/health", livenessHandler)
http.HandleFunc("/ready", readinessHandler)
```

## Additional Resources

- **Full reference**: Setup, code examples, decision trees (OTel vs Prometheus vs slog), anti-patterns, metric naming — see [reference.md](reference.md).
- **Official**: [OpenTelemetry Go](https://opentelemetry.io/docs/instrumentation/go/), [Prometheus client_golang](https://github.com/prometheus/client_golang), [log/slog](https://pkg.go.dev/log/slog).
