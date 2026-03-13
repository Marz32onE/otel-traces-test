package main

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Marz32onE/instrumentation-go/otel-mongo/otelmongo"
	"github.com/Marz32onE/instrumentation-go/otel-nats/oteljetstream"
	"github.com/Marz32onE/instrumentation-go/otel-nats/otelnats"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const (
	dbName   = "messaging"
	collName = "messages"
	subject  = "messages.db"
)

// initOTEL creates an OTLP TracerProvider and propagator, sets globals, and returns them
// so the app can pass them into instrumentation (otelnats, otelmongo) and defer shutdown.
func initOTEL(endpoint string, attrs ...attribute.KeyValue) (*sdktrace.TracerProvider, propagation.TextMapPropagator, error) {
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
		exp, err = otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(endpoint), otlptracehttp.WithInsecure())
	} else {
		exp, err = otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(endpoint), otlptracegrpc.WithInsecure())
	}
	if err != nil {
		return nil, nil, err
	}
	res, err := resource.New(ctx, resource.WithAttributes(attrs...))
	if err != nil {
		return nil, nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res))
	prop := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(prop)
	return tp, prop, nil
}

func useHTTPEndpoint(endpoint string) bool {
	s := strings.TrimSpace(endpoint)
	if s == "" {
		return false
	}
	if u, err := url.Parse(s); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		return true
	}
	_, port, _ := func(h string) (string, string, error) {
		u, err := url.Parse("//" + h)
		if err != nil {
			return "", "", err
		}
		return u.Hostname(), u.Port(), nil
	}(s)
	p, _ := strconv.Atoi(port)
	return p == 4318
}

func main() {
	ctx := context.Background()
	tp, prop, err := initOTEL("", attribute.String("service.name", "dbwatcher"), attribute.String("service.version", "0.0.1"))
	if err != nil {
		log.Fatalf("initOTEL: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(shutdownCtx)
	}()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}
	mongoClient, err := otelmongo.NewClient(mongoURI, otelmongo.WithTracerProvider(tp), otelmongo.WithPropagators(prop))
	if err != nil {
		log.Fatalf("MongoDB connect: %v", err)
	}
	defer func() {
		disconnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = mongoClient.Disconnect(disconnectCtx)
	}()

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}
	var natsConn *otelnats.Conn
	for i := 0; i < 10; i++ {
		natsConn, err = otelnats.ConnectWithOptions(natsURL, nil, otelnats.WithTracerProvider(tp), otelnats.WithPropagators(prop))
		if err == nil {
			break
		}
		log.Printf("Waiting for NATS... (%d/10)", i+1)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("NATS connect: %v", err)
	}
	defer natsConn.Close()

	js, err := oteljetstream.New(natsConn)
	if err != nil {
		log.Fatalf("JetStream: %v", err)
	}
	_, err = js.CreateOrUpdateStream(ctx, oteljetstream.StreamConfig{
		Name:     "MESSAGES",
		Subjects: []string{"messages.>"},
	})
	if err != nil {
		log.Printf("Stream create warning: %v", err)
	}

	coll := mongoClient.Database(dbName).Collection(collName)
	// Watch all CRUD: insert, update, replace, delete. UpdateLookup so update/replace include fullDocument.
	opts := options.ChangeStream().SetFullDocument(options.UpdateLookup)
	pipeline := mongo.Pipeline{} // no $match: receive all operation types

	// No separate Ping: open Watch with retry. Validates server up + change stream (replica set) in one step.
	var stream *otelmongo.ChangeStream
	for i := 0; i < 15; i++ {
		tryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		stream, err = coll.Watch(tryCtx, pipeline, opts)
		cancel()
		if err == nil {
			break
		}
		log.Printf("MongoDB change stream (%d/15): %v (ensure replica set)", i+1, err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("MongoDB change stream: %v", err)
	}
	defer func() { _ = stream.Close(ctx) }()

	// Graceful shutdown: cancel on SIGINT/SIGTERM so stream.Next returns and defers run
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Println("dbwatcher: watching MongoDB CRUD, publishing to NATS JetStream subject", subject)
	for {
		if !stream.Next(sigCtx) {
			if err := stream.Err(); err != nil {
				log.Printf("Change stream error: %v", err)
			}
			break
		}
		var event struct {
			OperationType string   `bson:"operationType"`
			FullDocument  bson.M   `bson:"fullDocument"`
			DocumentKey   bson.M   `bson:"documentKey"`
		}
		if err := stream.Decode(&event); err != nil {
			log.Printf("Decode: %v", err)
			continue
		}

		var payload []byte
		var pubCtx = sigCtx

		switch event.OperationType {
		case "insert", "update", "replace":
			text, _ := event.FullDocument["text"].(string)
			if text == "" {
				continue
			}
			rawDoc, _ := bson.Marshal(event.FullDocument)
			pubCtx = otelmongo.ContextFromDocument(sigCtx, rawDoc)
			payload = []byte(text)
		case "delete":
			idStr := ""
			if id, ok := event.DocumentKey["_id"]; ok {
				if oid, ok := id.(bson.ObjectID); ok {
					idStr = oid.Hex()
				}
			}
			payload, _ = json.Marshal(map[string]string{"op": "delete", "id": idStr})
			// delete has no fullDocument, so no _oteltrace to propagate
		default:
			continue
		}

		if _, err := js.Publish(pubCtx, subject, payload); err != nil {
			log.Printf("Publish to NATS: %v", err)
			continue
		}
		log.Printf("Forwarded to %s [%s]: %s", subject, event.OperationType, string(payload))
	}
}
