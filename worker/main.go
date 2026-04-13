package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	otelgorillaws "github.com/Marz32onE/instrumentation-go/otel-gorilla-ws"
	otelmongo "github.com/Marz32onE/instrumentation-go/otel-mongo/v2"
	"github.com/Marz32onE/instrumentation-go/otel-nats/oteljetstream"
	"github.com/Marz32onE/instrumentation-go/otel-nats/otelnats"
	"github.com/Marz32onE/otel-traces-test/pkg/otelsetup"
	"github.com/gorilla/websocket"
	nats "github.com/nats-io/nats.go"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var (
	// Accept otel-ws+json first so browsers using @marz32one/otel-rxjs-ws with protocol: ['json']
	// negotiate envelope mode (matches otel-gorilla-ws WriteMessage wire format).
	upgrader = otelgorillaws.Upgrader{
		CheckOrigin:  func(r *http.Request) bool { return true },
		Subprotocols: []string{"json"},
	}
	clients   = make(map[*otelgorillaws.Conn]bool)
	clientsMu sync.Mutex
)

// wsPayload is the application payload inside the otel-gorilla-ws envelope so the frontend can show body and which consumer delivered the message.
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

func wsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("Upgrade error: %v", err)
			return
		}
		defer func() {
			clientsMu.Lock()
			delete(clients, conn)
			clientsMu.Unlock()
			_ = conn.Close()
		}()

		clientsMu.Lock()
		clients[conn] = true
		clientsMu.Unlock()

		log.Printf("WebSocket client connected: %s", conn.RemoteAddr())
		readCtx := r.Context()
		for {
			if _, _, _, err := conn.ReadMessage(readCtx); err != nil {
				break
			}
		}
	}
}

// notifyBody is the JSON body for POST /notify (called by API via otelresty).
type notifyBody struct {
	Text string `json:"text"`
}

// dbNotify is published by dbwatcher on messages.db: change = fetch doc by id; delete = no fetch.
type dbNotify struct {
	Op string `json:"op"`
	ID string `json:"id"`
}

const mongoDBName, mongoColl = "messaging", "messages"

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
	tp, err := otelsetup.Init("",
		attribute.String("service.name", "worker"),
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
	msgColl := mongoClient.Database(mongoDBName).Collection(mongoColl)

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
		FilterSubject: "messages.new",
		AckPolicy:     oteljetstream.AckExplicitPolicy,
	})
	if err != nil {
		log.Fatalf("CreateOrUpdateConsumer(worker-consume): %v", err)
	}
	cc, err := consConsume.Consume(func(m oteljetstream.Msg) {
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
		FilterSubject: "messages.new",
		AckPolicy:     oteljetstream.AckExplicitPolicy,
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

	// 3) JetStream Fetch (single-fetch batch, trace per message). Run in a goroutine so
	// ListenAndServe below is not blocked; drain all of batch.Messages() per Fetch.
	consFetch, err := s.CreateOrUpdateConsumer(ctx, oteljetstream.ConsumerConfig{
		Durable:       "worker-fetch",
		FilterSubject: "messages.new",
		AckPolicy:     oteljetstream.AckExplicitPolicy,
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
			for m := range batch.Messages() {
				log.Printf("[Fetch] received: %s", string(m.Data()))
				broadcastWithTrace(m.Ctx, m.Data(), "Fetch")
				_ = m.Ack()
			}
			if batch.Error() != nil {
				log.Printf("[Fetch] batch error: %v", batch.Error())
			}
		}
	}()

	// 3.5) JetStream PushConsumer example for backend integration tests.
	// Uses an isolated subject to avoid duplicate broadcasts with existing pull consumers.
	pushCons, err := s.CreateOrUpdatePushConsumer(ctx, oteljetstream.ConsumerConfig{
		Durable:        "worker-push-example",
		DeliverSubject: "worker.push.deliver",
		FilterSubject:  "messages.push.it",
		AckPolicy:      oteljetstream.AckExplicitPolicy,
	})
	if err != nil {
		log.Printf("CreateOrUpdatePushConsumer(worker-push-example): %v", err)
	} else {
		pushCC, pushErr := pushCons.Consume(func(m oteljetstream.Msg) {
			log.Printf("[PushConsume] received: %s", string(m.Data()))
			broadcastWithTrace(m.Context(), m.Data(), "PushConsume")
			_ = m.Ack()
		})
		if pushErr != nil {
			log.Printf("PushConsume(worker-push-example): %v", pushErr)
		} else {
			defer pushCC.Stop()
		}
	}

	// 3.6) JetStream OrderedConsumer — ephemeral, no Ack needed, server auto-recreates on reconnect.
	// NamePrefix becomes the messaging.consumer.name span attribute.
	orderedCons, orderedErr := s.OrderedConsumer(ctx, oteljetstream.OrderedConsumerConfig{
		FilterSubjects: []string{"messages.new"},
		NamePrefix:     "worker-ordered",
	})
	if orderedErr != nil {
		log.Printf("OrderedConsumer: %v", orderedErr)
	} else {
		orderedCC, orderedConsumeErr := orderedCons.Consume(func(m oteljetstream.Msg) {
			log.Printf("[Ordered] received: %s", string(m.Data()))
			broadcastWithTrace(m.Context(), m.Data(), "Ordered")
			// OrderedConsumer does not require Ack
		})
		if orderedConsumeErr != nil {
			log.Printf("OrderedConsumer.Consume: %v", orderedConsumeErr)
		} else {
			defer orderedCC.Stop()
		}
	}

	// 4) Core NATS (fire-and-go)
	_, err = natsConn.Subscribe("messages.core", func(m otelnats.Msg) {
		log.Printf("Received core NATS message: %s", string(m.Msg.Data))
		broadcastWithTrace(m.Context(), m.Msg.Data, "Core")
	})
	if err != nil {
		log.Fatalf("Subscribe core: %v", err)
	}

	// 5) JetStream messages from dbwatcher (MongoDB change stream → messages.db)
	consDB, err := s.CreateOrUpdateConsumer(ctx, oteljetstream.ConsumerConfig{
		Durable:       "worker-db",
		FilterSubject: "messages.db",
		AckPolicy:     oteljetstream.AckExplicitPolicy,
	})
	if err != nil {
		log.Fatalf("CreateOrUpdateConsumer(worker-db): %v", err)
	}
	ccDB, err := consDB.Consume(func(m oteljetstream.Msg) {
		var n dbNotify
		if unmarshalErr := json.Unmarshal(m.Data(), &n); unmarshalErr != nil {
			log.Printf("[DB] bad JSON: %v", unmarshalErr)
			_ = m.Ack()
			return
		}
		switch n.Op {
		case "delete":
			log.Printf("[DB] delete id=%s", n.ID)
			broadcastWithTrace(m.Context(), m.Data(), "DB")
		case "change":
			if n.ID == "" {
				_ = m.Ack()
				return
			}
			oid, objectIDErr := bson.ObjectIDFromHex(n.ID)
			if objectIDErr != nil {
				log.Printf("[DB] invalid id %q: %v", n.ID, objectIDErr)
				_ = m.Ack()
				return
			}
			var doc struct {
				Text string `bson:"text"`
			}
			sr := msgColl.FindOneByID(m.Context(), oid)
			if decodeErr := sr.Decode(&doc); decodeErr != nil {
				log.Printf("[DB] FindOne %s: %v", n.ID, decodeErr)
				_ = m.Ack()
				return
			}
			if doc.Text == "" {
				_ = m.Ack()
				return
			}
			log.Printf("[DB] id=%s fetched, broadcasting", n.ID)
			broadcastWithTrace(m.Context(), []byte(doc.Text), "DB")
		default:
			log.Printf("[DB] unknown op %q", n.Op)
		}
		_ = m.Ack()
	})
	if err != nil {
		log.Fatalf("Consume(worker-db): %v", err)
	}
	defer ccDB.Stop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", wsHandler())
	mux.HandleFunc("POST /notify", notifyHandler)

	handler := otelhttp.NewHandler(mux, "worker")
	log.Println("Worker (net/http) starting on :8082")
	if err := http.ListenAndServe(":8082", handler); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
