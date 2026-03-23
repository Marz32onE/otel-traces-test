// Package otelsetup provides a shared way to create an OTLP TracerProvider and
// set the global TracerProvider and TextMapPropagator, following the same
// pattern as the instrumentation-go example packages. The application is
// responsible for initialization (per OTel contrib instrumentation guidelines).
//
// OTLP exporter options (see otlptracehttp):
//   - WithEndpoint("host:port"): host and port only; scheme/path come from defaults or env merge.
//   - WithEndpointURL("https://host:port/path"): full URL; sets host, path, and TLS vs insecure.
//
// This package uses WithEndpointURL for OTLP/HTTP so a single option overrides any malformed
// URLPath from OTEL_EXPORTER_OTLP_* env (e.g. bare hostname parsed as path). gRPC uses WithEndpoint(host:port).
package otelsetup

import (
	"context"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Init creates an OTLP TracerProvider, sets it and a default propagator (TraceContext + Baggage)
// on the global otel package, and returns the TracerProvider so the caller can defer Shutdown.
// Endpoint can be empty to use OTEL_EXPORTER_OTLP_ENDPOINT or "localhost:4317".
func Init(endpoint string, attrs ...attribute.KeyValue) (*sdktrace.TracerProvider, error) {
	if endpoint == "" {
		endpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	}
	if endpoint == "" {
		endpoint = "localhost:4317"
	}
	endpoint = strings.TrimSpace(endpoint)
	useHTTP := useHTTPEndpoint(endpoint)

	// otlptracehttp applies env (OTEL_EXPORTER_OTLP_*) first, then user options.
	// WithEndpoint(host:port) only sets Host; a bad env like "otel-collector" can leave URLPath
	// as "otel-collector/v1/traces". WithEndpointURL sets both Host and Path from one URL, so
	// we use it for HTTP and always pass a full http(s)://host:port (see otlpHTTPExporterURL).
	ctx := context.Background()

	var exp sdktrace.SpanExporter
	var err error
	if useHTTP {
		exp, err = otlptracehttp.New(ctx,
			otlptracehttp.WithEndpointURL(otlpHTTPExporterURL(endpoint)),
			otlptracehttp.WithInsecure(),
		)
	} else {
		exp, err = otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(otlpGRPCExporterEndpoint(endpoint)),
			otlptracegrpc.WithInsecure(),
		)
	}
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx, resource.WithAttributes(attrs...))
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return tp, nil
}

// Shutdown shuts down the TracerProvider with a timeout. Call in defer after Init.
func Shutdown(tp *sdktrace.TracerProvider) {
	if tp == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tp.Shutdown(ctx)
}

// otlpHTTPExporterURL builds a full OTLP/HTTP base URL for WithEndpointURL (scheme + host + port).
func otlpHTTPExporterURL(endpoint string) string {
	s := strings.TrimSpace(endpoint)
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return s
	}
	u, err := url.Parse("//" + s)
	if err != nil || u.Hostname() == "" {
		return "http://" + s
	}
	if p := u.Port(); p != "" {
		return "http://" + net.JoinHostPort(u.Hostname(), p)
	}
	return "http://" + net.JoinHostPort(u.Hostname(), "4318")
}

// otlpGRPCExporterEndpoint returns host:port for otlptracegrpc.WithEndpoint.
func otlpGRPCExporterEndpoint(endpoint string) string {
	s := strings.TrimSpace(endpoint)
	u, err := url.Parse("//" + s)
	if err != nil || u.Hostname() == "" {
		return s
	}
	if p := u.Port(); p != "" {
		return net.JoinHostPort(u.Hostname(), p)
	}
	return net.JoinHostPort(u.Hostname(), "4317")
}

// useHTTPEndpoint chooses OTLP/HTTP vs gRPC. Env without scheme and without port defaults to HTTP.
func useHTTPEndpoint(endpoint string) bool {
	s := strings.TrimSpace(endpoint)
	if s == "" {
		return false
	}
	if u, err := url.Parse(s); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		return true
	}
	if u, err := url.Parse("//" + s); err == nil {
		if u.Port() == "" {
			return true
		}
		if p, _ := strconv.Atoi(u.Port()); p == 4318 {
			return true
		}
	}
	return false
}
