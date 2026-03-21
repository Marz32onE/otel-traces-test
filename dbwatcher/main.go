package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	otelmongo "github.com/Marz32onE/instrumentation-go/otel-mongo/v2"
	"github.com/Marz32onE/instrumentation-go/otel-nats/oteljetstream"
	"github.com/Marz32onE/instrumentation-go/otel-nats/otelnats"
	"github.com/Marz32onE/otel-traces-test/pkg/otelsetup"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	dbName   = "messaging"
	collName = "messages"
	subject  = "messages.db"
)

func main() {
	ctx := context.Background()
	tp, err := otelsetup.Init("",
		attribute.String("service.name", "dbwatcher"),
		attribute.String("service.version", "0.0.1"),
	)
	if err != nil {
		log.Fatalf("otelsetup.Init: %v", err)
	}
	prop := otel.GetTextMapPropagator()
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
	tracer := otel.Tracer("dbwatcher")
	for {
		if !stream.Next(sigCtx) {
			if err := stream.Err(); err != nil {
				log.Printf("Change stream error: %v", err)
			}
			break
		}
		var event struct {
			FullDocument  bson.M `bson:"fullDocument"`
			DocumentKey   bson.M `bson:"documentKey"`
			OperationType string `bson:"operationType"`
		}
		eventCtx, err := stream.DecodeWithContext(sigCtx, &event)
		if err != nil {
			log.Printf("Decode: %v", err)
			continue
		}

		var payload []byte

		switch event.OperationType {
		case "insert", "update", "replace":
			text, _ := event.FullDocument["text"].(string)
			if text == "" {
				continue
			}
			idVal, ok := event.FullDocument["_id"]
			if !ok {
				continue
			}
			oid, ok := idVal.(bson.ObjectID)
			if !ok {
				continue
			}
			payload, _ = json.Marshal(map[string]string{"op": "change", "id": oid.Hex()})
		case "delete":
			idStr := ""
			if id, ok := event.DocumentKey["_id"]; ok {
				if oid, ok := id.(bson.ObjectID); ok {
					idStr = oid.Hex()
				}
			}
			payload, _ = json.Marshal(map[string]string{"op": "delete", "id": idStr})
		default:
			continue
		}

		spanOpts := []trace.SpanStartOption{
			trace.WithSpanKind(trace.SpanKindProducer),
			trace.WithAttributes(
				attribute.String("messaging.system", "nats"),
				attribute.String("messaging.destination.name", subject),
				attribute.String("messaging.operation.name", "publish"),
				attribute.String("db.operation.name", event.OperationType),
			),
		}
		pubCtx, span := tracer.Start(eventCtx, "publish "+subject, spanOpts...)
		if _, err := js.Publish(pubCtx, subject, payload); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.End()
			log.Printf("Publish to NATS: %v", err)
			continue
		}
		span.End()
		log.Printf("Forwarded to %s [%s] id-notify: %s", subject, event.OperationType, string(payload))
	}
}
