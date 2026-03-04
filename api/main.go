package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Marz32onE/natstrace/jetstreamtrace"
	natstrace "github.com/Marz32onE/natstrace/natstrace"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	nats "github.com/nats-io/nats.go"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/instrumentation/go.mongodb.org/mongo-driver/v2/mongo/otelmongo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const mongoDBName, mongoColl = "messaging", "messages"

var (
	natsConn    *natstrace.Conn
	jetstreamJS jetstreamtrace.JetStream
	mongoClient *mongo.Client
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

	prop := otel.GetTextMapPropagator()
	tp := otel.GetTracerProvider()
	var err error
	for i := 0; i < 10; i++ {
		natsConn, err = natstrace.Connect(natsURL, nil,
			natstrace.WithTracerProvider(tp),
			natstrace.WithPropagator(prop),
		)
		if err == nil {
			break
		}
		log.Printf("Waiting for NATS... (%d/10)", i+1)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer natsConn.Close()

	js, err := jetstreamtrace.New(natsConn)
	if err != nil {
		log.Fatalf("JetStream: %v", err)
	}
	_, err = js.CreateOrUpdateStream(context.Background(), jetstreamtrace.StreamConfig{
		Name:     "MESSAGES",
		Subjects: []string{"messages.>"},
	})
	if err != nil {
		log.Printf("Stream creation warning: %v", err)
	}
	jetstreamJS = js

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}
	tpForMongo := otel.GetTracerProvider()
	monitor := otelmongo.NewMonitor(otelmongo.WithTracerProvider(tpForMongo))
	clientOpts := options.Client().ApplyURI(mongoURI).SetMonitor(monitor)
	mongoClient, err = mongo.Connect(clientOpts)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer func() {
		_ = mongoClient.Disconnect(context.Background())
	}()
	if err = mongoClient.Ping(context.Background(), nil); err != nil {
		log.Fatalf("MongoDB ping: %v", err)
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

	r.POST("/api/message", handleMessage)            // JetStream (natstrace)
	r.POST("/api/message-core", handleMessageCore)   // Core NATS fire-and-go
	r.POST("/api/message-mongo", handleMessageMongo) // Store to MongoDB

	log.Println("API server starting on :8088")
	if err := r.Run(":8088"); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

type MessageRequest struct {
	Text string `json:"text" binding:"required"`
}

func getTraceIDFromContext(ctx context.Context) string {
	if span := trace.SpanFromContext(ctx); span.SpanContext().HasTraceID() {
		return span.SpanContext().TraceID().String()
	}
	return ""
}

func handleMessage(c *gin.Context) {
	var req MessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	if _, err := jetstreamJS.Publish(ctx, "messages.new", []byte(req.Text)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to publish message"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":   "published",
		"trace_id": getTraceIDFromContext(ctx),
		"endpoint": "JetStream",
	})
}

func handleMessageCore(c *gin.Context) {
	var req MessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	if err := natsConn.Publish(ctx, "messages.core", []byte(req.Text)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to publish message"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":   "published",
		"trace_id": getTraceIDFromContext(ctx),
		"endpoint": "Core",
	})
}

func handleMessageMongo(c *gin.Context) {
	var req MessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	doc := bson.M{"text": req.Text, "createdAt": time.Now()}
	coll := mongoClient.Database(mongoDBName).Collection(mongoColl)
	if _, err := coll.InsertOne(ctx, doc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store message"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":   "stored",
		"trace_id": getTraceIDFromContext(ctx),
		"endpoint": "MongoDB",
	})
}
