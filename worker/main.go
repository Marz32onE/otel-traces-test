package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	natstrace "github.com/Marz32onE/nats.trace.go"
	nats "github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	clients   = make(map[*websocket.Conn]bool)
	clientsMu sync.Mutex
)

// wsPayload is sent over WebSocket so the frontend can extract trace context and complete the trace.
type wsPayload struct {
	Traceparent string `json:"traceparent"`
	Tracestate  string `json:"tracestate,omitempty"`
	Body        string `json:"body"`
}

func broadcastWithTrace(ctx context.Context, body []byte) {
	carrier := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(carrier))
	payload := wsPayload{
		Traceparent: carrier["traceparent"],
		Tracestate:  carrier["tracestate"],
		Body:        string(body),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(payload.Body)
	}
	clientsMu.Lock()
	defer clientsMu.Unlock()
	for conn := range clients {
		if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
			log.Printf("WebSocket write error: %v", err)
			conn.Close()
			delete(clients, conn)
		}
	}
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		return
	}
	defer func() {
		clientsMu.Lock()
		delete(clients, conn)
		clientsMu.Unlock()
		conn.Close()
	}()

	clientsMu.Lock()
	clients[conn] = true
	clientsMu.Unlock()

	log.Printf("WebSocket client connected: %s", conn.RemoteAddr())
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

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
			attribute.String("service.name", "worker"),
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

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = nats.DefaultURL
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
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer natsConn.Close()

	nc := natsConn.NatsConn()
	js, err := nc.JetStream()
	if err != nil {
		log.Fatalf("JetStream: %v", err)
	}
	_, err = js.AddStream(&nats.StreamConfig{
		Name:     "MESSAGES",
		Subjects: []string{"messages.>"},
	})
	if err != nil && err != nats.ErrStreamNameAlreadyInUse {
		log.Printf("Stream creation warning: %v", err)
	}

	_, err = natsConn.SubscribeJetStream("messages.new", func(ctx context.Context, msg *nats.Msg) {
		log.Printf("Received NATS message: %s", string(msg.Data))
		broadcastWithTrace(ctx, msg.Data)
		_ = msg.Ack()
	})
	if err != nil {
		log.Fatalf("Subscribe JetStream: %v", err)
	}

	_, err = natsConn.Subscribe("messages.core", func(ctx context.Context, msg *nats.Msg) {
		log.Printf("Received core NATS message: %s", string(msg.Data))
		broadcastWithTrace(ctx, msg.Data)
	})
	if err != nil {
		log.Fatalf("Subscribe core: %v", err)
	}

	http.HandleFunc("/ws", wsHandler)
	log.Println("WebSocket worker starting on :8082")
	if err := http.ListenAndServe(":8082", nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
