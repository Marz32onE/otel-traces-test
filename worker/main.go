package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Marz32onE/instrumentation-go/otel-nats/oteljetstream"
	"github.com/Marz32onE/instrumentation-go/otel-nats/otelnats"
	"github.com/Marz32onE/instrumentation-go/otel-websocket"
	"github.com/gorilla/websocket"
	nats "github.com/nats-io/nats.go"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	clients   = make(map[*otelwebsocket.Conn]bool)
	clientsMu sync.Mutex
)

// wsPayload is the application payload inside the otelwebsocket envelope so the frontend can show body and which consumer delivered the message.
type wsPayload struct {
	Body string `json:"body"`
	Api  string `json:"api,omitempty"` // consumer type postfix for verification, e.g. "Consume", "Messages"
}

func broadcastWithTrace(ctx context.Context, body []byte, apiName string) {
	payload := wsPayload{Body: string(body), Api: apiName}
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(payload.Body)
	}
	clientsMu.Lock()
	defer clientsMu.Unlock()
	for conn := range clients {
		if err := conn.WriteMessage(ctx, websocket.TextMessage, raw); err != nil {
			log.Printf("WebSocket write error: %v", err)
			_ = conn.Close()
			delete(clients, conn)
		}
	}
}

// initOTEL creates an OTLP TracerProvider and propagator, sets globals, and returns them
// so the app can pass them into instrumentation (otelnats, otelwebsocket) and defer shutdown.
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

func wsHandler(tp *sdktrace.TracerProvider, prop propagation.TextMapPropagator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("Upgrade error: %v", err)
			return
		}
		conn := otelwebsocket.NewConn(raw, otelwebsocket.WithTracerProvider(tp), otelwebsocket.WithPropagators(prop))
		defer func() {
			clientsMu.Lock()
			delete(clients, conn)
			clientsMu.Unlock()
			_ = conn.Close()
		}()

		clientsMu.Lock()
		clients[conn] = true
		clientsMu.Unlock()

		log.Printf("WebSocket client connected: %s", raw.RemoteAddr())
		for {
			if _, _, err := raw.ReadMessage(); err != nil {
				break
			}
		}
	}
}

// notifyBody is the JSON body for POST /notify (called by API via otelresty).
type notifyBody struct {
	Text string `json:"text"`
}

func notifyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req notifyBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	log.Printf("[Notify] received: %s", req.Text)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "text": req.Text})
}

func main() {
	ctx := context.Background()
	tp, prop, err := initOTEL("", attribute.String("service.name", "worker"), attribute.String("service.version", "0.0.1"))
	if err != nil {
		log.Fatalf("initOTEL: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(shutdownCtx)
	}()

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = nats.DefaultURL
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
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer natsConn.Close()

	js, err := oteljetstream.New(natsConn)
	if err != nil {
		log.Fatalf("JetStream: %v", err)
	}
	s, err := js.CreateOrUpdateStream(ctx, oteljetstream.StreamConfig{
		Name:     "MESSAGES",
		Subjects: []string{"messages.>"},
	})
	if err != nil {
		log.Printf("Stream creation warning: %v", err)
	}

	// 1) JetStream Consume (callback) — 消費者區分由 otelnats 的 messaging.consumer.name 表示 (worker-consume)
	consConsume, err := s.CreateOrUpdateConsumer(ctx, oteljetstream.ConsumerConfig{
		Durable:       "worker-consume",
		FilterSubject:  "messages.new",
		AckPolicy:      oteljetstream.AckExplicitPolicy,
	})
	if err != nil {
		log.Fatalf("CreateOrUpdateConsumer(worker-consume): %v", err)
	}
	cc, err := consConsume.Consume(func(m oteljetstream.MsgWithContext) {
		log.Printf("[Consume] received: %s", string(m.Data()))
		broadcastWithTrace(m.Context(), m.Data(), "Consume")
		_ = m.Ack()
	})
	if err != nil {
		log.Fatalf("Consume: %v", err)
	}
	defer cc.Stop()

	// 2) JetStream Messages() iterator — 消費者區分: messaging.consumer.name = worker-messages；broadcast 後綴驗證用
	consMessages, err := s.CreateOrUpdateConsumer(ctx, oteljetstream.ConsumerConfig{
		Durable:       "worker-messages",
		FilterSubject:  "messages.new",
		AckPolicy:      oteljetstream.AckExplicitPolicy,
	})
	if err != nil {
		log.Fatalf("CreateOrUpdateConsumer(worker-messages): %v", err)
	}
	iter, err := consMessages.Messages()
	if err != nil {
		log.Fatalf("Messages: %v", err)
	}
	defer iter.Stop()
	go func() {
		for {
			msgCtx, msg, nextErr := iter.Next()
			if nextErr != nil {
				return
			}
			log.Printf("[Messages] received: %s", string(msg.Data()))
			broadcastWithTrace(msgCtx, msg.Data(), "Messages")
			_ = msg.Ack()
		}
	}()

	// 3) JetStream Fetch (single-fetch batch, trace per message)
	consFetch, err := s.CreateOrUpdateConsumer(ctx, oteljetstream.ConsumerConfig{
		Durable:       "worker-fetch",
		FilterSubject:  "messages.new",
		AckPolicy:      oteljetstream.AckExplicitPolicy,
	})
	if err != nil {
		log.Fatalf("CreateOrUpdateConsumer(worker-fetch): %v", err)
	}
	go func() {
		for {
			batch, fetchErr := consFetch.Fetch(5)
			if fetchErr != nil {
				continue
			}
			for m := range batch.MessagesWithContext() {
				log.Printf("[Fetch] received: %s", string(m.Msg.Data()))
				broadcastWithTrace(m.Ctx, m.Msg.Data(), "Fetch")
				_ = m.Msg.Ack()
			}
			if batch.Error() != nil {
				log.Printf("[Fetch] batch error: %v", batch.Error())
			}
		}
	}()

	// 4) Core NATS (fire-and-go)
	_, err = natsConn.Subscribe("messages.core", func(m otelnats.MsgWithContext) {
		log.Printf("Received core NATS message: %s", string(m.Msg.Data))
		broadcastWithTrace(m.Context(), m.Msg.Data, "Core")
	})
	if err != nil {
		log.Fatalf("Subscribe core: %v", err)
	}

	// 5) JetStream messages from dbwatcher (MongoDB change stream → messages.db)
	consDB, err := s.CreateOrUpdateConsumer(ctx, oteljetstream.ConsumerConfig{
		Durable:       "worker-db",
		FilterSubject:  "messages.db",
		AckPolicy:      oteljetstream.AckExplicitPolicy,
	})
	if err != nil {
		log.Fatalf("CreateOrUpdateConsumer(worker-db): %v", err)
	}
	ccDB, err := consDB.Consume(func(m oteljetstream.MsgWithContext) {
		log.Printf("[DB] received: %s", string(m.Data()))
		broadcastWithTrace(m.Context(), m.Data(), "DB")
		_ = m.Ack()
	})
	if err != nil {
		log.Fatalf("Consume(worker-db): %v", err)
	}
	defer ccDB.Stop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", wsHandler(tp, prop))
	mux.HandleFunc("POST /notify", notifyHandler)

	handler := otelhttp.NewHandler(mux, "worker")
	log.Println("Worker (net/http) starting on :8082")
	if err := http.ListenAndServe(":8082", handler); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
