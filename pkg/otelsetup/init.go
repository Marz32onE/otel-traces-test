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

	otelmongo "github.com/Marz32onE/instrumentation-go/otel-mongo/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// InitOption configures InitWithOptions.
type InitOption interface {
	apply(*initConfig)
}

type initOptionFunc func(*initConfig)

func (f initOptionFunc) apply(c *initConfig) { f(c) }

type initConfig struct {
	skipDBOperations []string
}

func newInitConfig(opts []InitOption) *initConfig {
	cfg := &initConfig{}
	for _, o := range opts {
		o.apply(cfg)
	}
	return cfg
}

// WithSkipDBOperations drops spans whose db.operation.name matches one of skipOps
// (case-insensitive), e.g. "getMore".
func WithSkipDBOperations(skipOps []string) InitOption {
	return initOptionFunc(func(c *initConfig) {
		c.skipDBOperations = append([]string(nil), skipOps...)
	})
}

// Init creates an OTLP TracerProvider, sets it and a default propagator (TraceContext + Baggage)
// on the global otel package, and returns the TracerProvider so the caller can defer Shutdown.
// Endpoint can be empty to use OTEL_EXPORTER_OTLP_ENDPOINT or "localhost:4317".
func Init(endpoint string, attrs ...attribute.KeyValue) (*sdktrace.TracerProvider, error) {
	return InitWithOptions(endpoint, attrs)
}

// InitWithOptions is like Init, but accepts optional setup behaviors such as
// filtering noisy DB operation spans before export.
func InitWithOptions(endpoint string, attrs []attribute.KeyValue, opts ...InitOption) (*sdktrace.TracerProvider, error) {
	cfg := newInitConfig(opts)
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
	if len(cfg.skipDBOperations) > 0 {
		exp = otelmongo.SkipDBOperationsExporter(exp, cfg.skipDBOperations)
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
