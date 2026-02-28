package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var (
	ncConn  *nats.Conn
	jsCtx   nats.JetStreamContext
	jsNew   jetstream.JetStream
	tracer  trace.Tracer
)

func initTracer() func() {
	ctx := context.Background()

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:4317"
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		log.Fatalf("Failed to create OTLP exporter: %v", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", "api"),
			attribute.String("service.version", "0.0.1"),
		),
	)
	if err != nil {
		log.Fatalf("Failed to create resource: %v", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tracer = tp.Tracer("api")

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tp.Shutdown(shutdownCtx)
	}
}

func main() {
	shutdown := initTracer()
	defer shutdown()

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = nats.DefaultURL
	}

	var nc *nats.Conn
	var err error
	for i := 0; i < 10; i++ {
		nc, err = nats.Connect(natsURL)
		if err == nil {
			break
		}
		log.Printf("Waiting for NATS... (%d/10)", i+1)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	ncConn = nc
	defer nc.Close()

	jsCtx, err = nc.JetStream()
	if err != nil {
		log.Fatalf("Failed to get JetStream context: %v", err)
	}

	_, err = jsCtx.AddStream(&nats.StreamConfig{
		Name:     "MESSAGES",
		Subjects: []string{"messages.>"},
	})
	if err != nil && err != nats.ErrStreamNameAlreadyInUse {
		log.Printf("Stream creation warning: %v", err)
	}

	jsNew, err = jetstream.New(nc)
	if err != nil {
		log.Fatalf("Failed to create jetstream.JetStream: %v", err)
	}
	ctx := context.Background()
	_, err = jsNew.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "MESSAGES",
		Subjects: []string{"messages.>"},
	})
	if err != nil {
		log.Printf("JetStream CreateOrUpdateStream warning: %v", err)
	}

	r := gin.Default()
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "traceparent", "tracestate"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}))
	r.Use(otelgin.Middleware("api"))

	r.POST("/api/message", handleMessage)
	r.POST("/api/message-v2", handleMessageV2)
	r.POST("/api/message-core", handleMessageCore)

	log.Println("API server starting on :8081")
	if err := r.Run(":8081"); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

type MessageRequest struct {
	Text string `json:"text" binding:"required"`
}

// Tracing: otelgin.Middleware creates the HTTP span (e.g. "POST /api/message"). Handlers add a child
// "send <subject>" span with SpanKindProducer and messaging.* attributes so the trace shows both
// transport (HTTP) and domain (NATS publish). Keeping both is intentional: Gin gives route/status,
// handler gives subject, payload size, and nats.api (core vs jetstream).
func handleMessage(c *gin.Context) {
	var req MessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	subject := "messages.new"
	payload := []byte(req.Text)
	_, span := tracer.Start(ctx, "send "+subject,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "nats"),
			attribute.String("messaging.operation.name", "send"),
			attribute.String("messaging.destination.name", subject),
			attribute.Int("messaging.message.body.size", len(payload)),
			attribute.String("message.content", req.Text),
		),
	)
	defer span.End()

	_, err := jsCtx.Publish(subject, payload)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to publish message"})
		return
	}

	span.SetStatus(codes.Ok, "")
	c.JSON(http.StatusOK, gin.H{"status": "published"})
}

// handleMessageV2 publishes to NATS using jetstream package (Publisher interface).
// Same behaviour as handleMessage; only the NATS client API differs.
func handleMessageV2(c *gin.Context) {
	var req MessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	subject := "messages.new"
	payload := []byte(req.Text)
	_, span := tracer.Start(ctx, "send "+subject,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "nats"),
			attribute.String("messaging.operation.name", "send"),
			attribute.String("messaging.destination.name", subject),
			attribute.Int("messaging.message.body.size", len(payload)),
			attribute.String("message.content", req.Text),
			attribute.String("nats.api", "jetstream.Publisher"),
		),
	)
	defer span.End()

	_, err := jsNew.Publish(ctx, subject, payload)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to publish message"})
		return
	}

	span.SetStatus(codes.Ok, "")
	c.JSON(http.StatusOK, gin.H{"status": "published"})
}

// handleMessageCore publishes using core NATS (nc.Publish), not JetStream.
// Uses subject "messages.core" so the worker's core subscriber is the only one that receives it (avoids duplicate display with JetStream).
func handleMessageCore(c *gin.Context) {
	var req MessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	subject := "messages.core"
	payload := []byte(req.Text)
	_, span := tracer.Start(ctx, "send "+subject,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "nats"),
			attribute.String("messaging.operation.name", "send"),
			attribute.String("messaging.destination.name", subject),
			attribute.Int("messaging.message.body.size", len(payload)),
			attribute.String("message.content", req.Text),
			attribute.String("nats.api", "core"),
		),
	)
	defer span.End()

	err := ncConn.Publish(subject, payload)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to publish message"})
		return
	}

	span.SetStatus(codes.Ok, "")
	c.JSON(http.StatusOK, gin.H{"status": "published"})
}
