package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/Marz32onE/natstrace/jetstreamtrace"
	natstrace "github.com/Marz32onE/natstrace/natstrace"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.opentelemetry.io/contrib/instrumentation/go.mongodb.org/mongo-driver/v2/mongo/otelmongo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const (
	dbName   = "messaging"
	collName = "messages"
	subject  = "messages.db"
)

func main() {
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
		log.Fatalf("OTLP exporter: %v", err)
	}
	res, _ := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", "dbwatcher"),
			attribute.String("service.version", "0.0.1"),
		),
	)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(shutdownCtx)
	}()

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}
	monitor := otelmongo.NewMonitor(otelmongo.WithTracerProvider(tp))
	clientOpts := options.Client().ApplyURI(mongoURI).SetMonitor(monitor)
	mongoClient, err := mongo.Connect(clientOpts)
	if err != nil {
		log.Fatalf("MongoDB connect: %v", err)
	}
	defer func() {
		_ = mongoClient.Disconnect(context.Background())
	}()

	for i := 0; i < 15; i++ {
		if err = mongoClient.Ping(ctx, nil); err == nil {
			break
		}
		log.Printf("MongoDB ping (%d/15): %v", i+1, err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("MongoDB ping: %v", err)
	}

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}
	prop := otel.GetTextMapPropagator()
	var natsConn *natstrace.Conn
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
	// Match only insert events to forward message text to NATS
	pipeline := mongo.Pipeline{bson.D{{Key: "$match", Value: bson.D{{Key: "operationType", Value: "insert"}}}}}
	stream, err := coll.Watch(ctx, pipeline)
	if err != nil {
		log.Fatalf("Change stream: %v (ensure MongoDB is a replica set)", err)
	}
	defer stream.Close(ctx)

	log.Println("dbwatcher: watching MongoDB messages, publishing to NATS JetStream subject", subject)
	for {
		if !stream.Next(ctx) {
			if err := stream.Err(); err != nil {
				log.Printf("Change stream error: %v", err)
			}
			break
		}
		var event struct {
			FullDocument bson.M `bson:"fullDocument"`
		}
		if err := stream.Decode(&event); err != nil {
			log.Printf("Decode: %v", err)
			continue
		}
		text, _ := event.FullDocument["text"].(string)
		if text == "" {
			continue
		}
		if _, err := js.Publish(ctx, subject, []byte(text)); err != nil {
			log.Printf("Publish to NATS: %v", err)
			continue
		}
		log.Printf("Forwarded to %s: %s", subject, text)
	}
}
