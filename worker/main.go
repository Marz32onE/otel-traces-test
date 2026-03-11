package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Marz32onE/natstrace/jetstreamtrace"
	natstrace "github.com/Marz32onE/natstrace/natstrace"
	"github.com/Marz32onE/otelwebsocket"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	nats "github.com/nats-io/nats.go"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel/attribute"
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

// wsHandlerGin handles WebSocket upgrade; after Upgrade the connection is hijacked, so we do not use c.JSON.
func wsHandlerGin(c *gin.Context) {
	raw, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		return
	}
	conn := otelwebsocket.NewConn(raw)
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

// notifyBody is the JSON body for POST /notify (called by API via otelresty).
type notifyBody struct {
	Text string `json:"text"`
}

// notifyHandler handles POST /notify from API (otelresty demo); returns 200 with optional echo.
func notifyHandler(c *gin.Context) {
	var req notifyBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	log.Printf("[Notify] received: %s", req.Text)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "text": req.Text})
}

func main() {
	ctx := context.Background()
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if err := natstrace.InitTracer(endpoint,
		attribute.String("service.name", "worker"),
		attribute.String("service.version", "0.0.1"),
	); err != nil {
		log.Fatalf("InitTracer: %v", err)
	}
	defer natstrace.ShutdownTracer()

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = nats.DefaultURL
	}
	var err error
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
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer natsConn.Close()

	js, err := jetstreamtrace.New(natsConn)
	if err != nil {
		log.Fatalf("JetStream: %v", err)
	}
	s, err := js.CreateOrUpdateStream(ctx, jetstreamtrace.StreamConfig{
		Name:     "MESSAGES",
		Subjects: []string{"messages.>"},
	})
	if err != nil {
		log.Printf("Stream creation warning: %v", err)
	}

	// 1) JetStream Consume (callback) — 消費者區分由 natstrace 的 messaging.consumer.name 表示 (worker-consume)
	consConsume, err := s.CreateOrUpdateConsumer(ctx, jetstreamtrace.ConsumerConfig{
		Durable:       "worker-consume",
		FilterSubject: "messages.new",
		AckPolicy:     jetstreamtrace.AckExplicitPolicy,
	})
	if err != nil {
		log.Fatalf("CreateOrUpdateConsumer(worker-consume): %v", err)
	}
	cc, err := consConsume.Consume(func(m jetstreamtrace.MsgWithContext) {
		log.Printf("[Consume] received: %s", string(m.Data()))
		broadcastWithTrace(m.Context(), m.Data(), "Consume")
		_ = m.Ack()
	})
	if err != nil {
		log.Fatalf("Consume: %v", err)
	}
	defer cc.Stop()

	// 2) JetStream Messages() iterator — 消費者區分: messaging.consumer.name = worker-messages；broadcast 後綴驗證用
	consMessages, err := s.CreateOrUpdateConsumer(ctx, jetstreamtrace.ConsumerConfig{
		Durable:       "worker-messages",
		FilterSubject: "messages.new",
		AckPolicy:     jetstreamtrace.AckExplicitPolicy,
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
	consFetch, err := s.CreateOrUpdateConsumer(ctx, jetstreamtrace.ConsumerConfig{
		Durable:       "worker-fetch",
		FilterSubject: "messages.new",
		AckPolicy:     jetstreamtrace.AckExplicitPolicy,
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
	_, err = natsConn.Subscribe("messages.core", func(m natstrace.MsgWithContext) {
		log.Printf("Received core NATS message: %s", string(m.Msg.Data))
		broadcastWithTrace(m.Context(), m.Msg.Data, "Core")
	})
	if err != nil {
		log.Fatalf("Subscribe core: %v", err)
	}

	// 5) JetStream messages from dbwatcher (MongoDB change stream → messages.db)
	consDB, err := s.CreateOrUpdateConsumer(ctx, jetstreamtrace.ConsumerConfig{
		Durable:       "worker-db",
		FilterSubject: "messages.db",
		AckPolicy:     jetstreamtrace.AckExplicitPolicy,
	})
	if err != nil {
		log.Fatalf("CreateOrUpdateConsumer(worker-db): %v", err)
	}
	ccDB, err := consDB.Consume(func(m jetstreamtrace.MsgWithContext) {
		log.Printf("[DB] received: %s", string(m.Data()))
		broadcastWithTrace(m.Context(), m.Data(), "DB")
		_ = m.Ack()
	})
	if err != nil {
		log.Fatalf("Consume(worker-db): %v", err)
	}
	defer ccDB.Stop()

	r := gin.Default()
	r.Use(otelgin.Middleware("worker"))
	r.GET("/ws", wsHandlerGin)
	r.POST("/notify", notifyHandler)

	log.Println("Worker (Gin) starting on :8082")
	if err := r.Run(":8082"); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
