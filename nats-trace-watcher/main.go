package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Marz32onE/instrumentation-go/otel-nats/otelnats"
	"github.com/Marz32onE/otel-traces-test/pkg/otelsetup"
	nats "github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/attribute"
)

func main() {
	tp, err := otelsetup.Init("",
		attribute.String("service.name", "nats-trace-watcher"),
		attribute.String("service.version", "0.0.1"),
	)
	if err != nil {
		log.Fatalf("otelsetup.Init: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(ctx)
	}()

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = nats.DefaultURL
	}
	traceSubject := os.Getenv("NATS_TRACE_DEST")
	if traceSubject == "" {
		traceSubject = "nats.trace.events"
	}

	var conn *otelnats.Conn
	for i := 0; i < 10; i++ {
		conn, err = otelnats.Connect(natsURL)
		if err == nil {
			break
		}
		log.Printf("Waiting for NATS... (%d/10)", i+1)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer conn.Close()

	sub, err := otelnats.SubscribeTraceEvents(conn, traceSubject)
	if err != nil {
		log.Fatalf("SubscribeTraceEvents: %v", err)
	}
	defer sub.Unsubscribe() //nolint:errcheck // best-effort cleanup on shutdown

	log.Printf("nats-trace-watcher: listening on %q", traceSubject)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("nats-trace-watcher: shutting down")
}
