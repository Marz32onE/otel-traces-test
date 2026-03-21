// Package otelsetup provides a shared way to create an OTLP TracerProvider and
// set the global TracerProvider and TextMapPropagator, following the same
// pattern as the instrumentation-go example packages. The application is
// responsible for initialization (per OTel Go Contrib instrumentation guidelines).
package otelsetup

import (
	"context"
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
	useHTTP := useHTTPEndpoint(endpoint)
	ctx := context.Background()

	var exp sdktrace.SpanExporter
	var err error
	if useHTTP {
		exp, err = otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(endpoint),
			otlptracehttp.WithInsecure(),
		)
	} else {
		exp, err = otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(endpoint),
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

func useHTTPEndpoint(endpoint string) bool {
	s := strings.TrimSpace(endpoint)
	if s == "" {
		return false
	}
	if u, err := url.Parse(s); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		return true
	}
	_, port, _ := splitHostPort(s)
	p, _ := strconv.Atoi(port)
	return p == 4318
}

func splitHostPort(hostport string) (host, port string, err error) {
	u, err := url.Parse("//" + hostport)
	if err != nil {
		return "", "", err
	}
	return u.Hostname(), u.Port(), nil
}
