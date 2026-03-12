package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Marz32onE/mongodbtrace/mongotrace"
	"github.com/Marz32onE/natstrace/jetstreamtrace"
	natstrace "github.com/Marz32onE/natstrace/natstrace"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.opentelemetry.io/otel/attribute"
)

const (
	dbName   = "messaging"
	collName = "messages"
	subject  = "messages.db"
)

func main() {
	ctx := context.Background()
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	attrs := []attribute.KeyValue{
		attribute.String("service.name", "dbwatcher"),
		attribute.String("service.version", "0.0.1"),
	}
	if _, err := mongotrace.InitTracer(endpoint, attrs); err != nil {
		log.Fatalf("mongotrace.InitTracer: %v", err)
	}
	if err := natstrace.InitTracer(endpoint, attrs); err != nil {
		log.Fatalf("natstrace.InitTracer: %v", err)
	}
	defer mongotrace.ShutdownTracer()
	defer natstrace.ShutdownTracer()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}
	mongoClient, err := mongotrace.NewClient(mongoURI)
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
	var natsConn *natstrace.Conn
	for i := 0; i < 10; i++ {
		natsConn, err = natstrace.Connect(natsURL, nil)
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

	js, err := jetstreamtrace.New(natsConn)
	if err != nil {
		log.Fatalf("JetStream: %v", err)
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstreamtrace.StreamConfig{
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
	var stream *mongo.ChangeStream
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
			pubCtx = mongotrace.ContextFromDocument(sigCtx, rawDoc)
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
