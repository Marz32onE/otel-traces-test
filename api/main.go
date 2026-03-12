package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Marz32onE/mongodbtrace/mongotrace"
	"github.com/Marz32onE/natstrace/jetstreamtrace"
	natstrace "github.com/Marz32onE/natstrace/natstrace"
	"github.com/dubonzi/otelresty"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	nats "github.com/nats-io/nats.go"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const mongoDBName, mongoColl = "messaging", "messages"

var (
	natsConn     *natstrace.Conn
	jetstreamJS  jetstreamtrace.JetStream
	mongoClient  *mongotrace.Client
	workerClient *resty.Client
)

func main() {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	attrs := []attribute.KeyValue{
		attribute.String("service.name", "api"),
		attribute.String("service.version", "0.0.1"),
	}
	if err := natstrace.InitTracer(endpoint, attrs); err != nil {
		log.Fatalf("natstrace.InitTracer: %v", err)
	}
	if _, err := mongotrace.InitTracer(endpoint, attrs); err != nil {
		log.Fatalf("mongotrace.InitTracer: %v", err)
	}
	defer natstrace.ShutdownTracer()
	defer mongotrace.ShutdownTracer()

	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = nats.DefaultURL
	}
	var err error
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
	_, err = js.CreateOrUpdateStream(context.Background(), jetstreamtrace.StreamConfig{
		Name:     "MESSAGES",
		Subjects: []string{"messages.>"},
	})
	if err != nil {
		log.Printf("Stream creation warning: %v", err)
	}
	jetstreamJS = js

	workerURL := os.Getenv("WORKER_URL")
	if workerURL == "" {
		workerURL = "http://worker:8082"
	}
	base := resty.New().SetBaseURL(workerURL)
	otelresty.TraceClient(base) // github.com/dubonzi/otelresty: spans + trace propagation
	workerClient = base

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}
	mongoClient, err = mongotrace.NewClient(mongoURI)
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

	r.POST("/api/message", handleMessage)                  // JetStream (natstrace)
	r.POST("/api/message-core", handleMessageCore)         // Core NATS fire-and-go
	r.POST("/api/message-mongo", handleMessageMongo)       // MongoDB Insert
	r.POST("/api/message-via-worker", handleMessageViaWorker) // HTTP to Worker (otelresty)
	r.POST("/api/message-mongo-update", handleMessageMongoUpdate)
	r.POST("/api/message-mongo-read", handleMessageMongoRead)
	r.POST("/api/message-mongo-delete", handleMessageMongoDelete)

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

// handleMessageViaWorker calls Worker POST /notify via otelresty (HTTP with trace propagation).
func handleMessageViaWorker(c *gin.Context) {
	var req MessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	resp, err := workerClient.R().SetContext(ctx).SetBody(gin.H{"text": req.Text}).Post("/notify")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to call worker: " + err.Error()})
		return
	}
	if resp.IsError() {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "worker returned " + resp.Status()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":   "ok",
		"trace_id": getTraceIDFromContext(ctx),
		"endpoint": "Worker HTTP (otelresty)",
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
	res, err := coll.InsertOne(ctx, doc)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store message"})
		return
	}
	idHex := ""
	if oid, ok := res.InsertedID.(bson.ObjectID); ok {
		idHex = oid.Hex()
	}
	c.JSON(http.StatusOK, gin.H{
		"status":   "stored",
		"trace_id": getTraceIDFromContext(ctx),
		"endpoint": "MongoDB",
		"id":       idHex,
	})
}

// MongoIDRequest is used for update/read/delete by document _id.
type MongoIDRequest struct {
	ID string `json:"id" binding:"required"`
}

// MongoUpdateRequest adds optional text for update.
type MongoUpdateRequest struct {
	ID   string `json:"id" binding:"required"`
	Text string `json:"text"`
}

func handleMessageMongoUpdate(c *gin.Context) {
	var req MongoUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	oid, err := bson.ObjectIDFromHex(req.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	ctx := c.Request.Context()
	coll := mongoClient.Database(mongoDBName).Collection(mongoColl)
	update := bson.M{"$set": bson.M{"text": req.Text, "updatedAt": time.Now()}}
	res, err := coll.UpdateOne(ctx, bson.M{"_id": oid}, update)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update"})
		return
	}
	if res.MatchedCount == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":   "updated",
		"trace_id": getTraceIDFromContext(ctx),
		"endpoint": "MongoDB Update",
	})
}

func handleMessageMongoRead(c *gin.Context) {
	var req MongoIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	oid, err := bson.ObjectIDFromHex(req.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	ctx := c.Request.Context()
	coll := mongoClient.Database(mongoDBName).Collection(mongoColl)
	var doc struct {
		Text      string    `bson:"text"`
		CreatedAt time.Time `bson:"createdAt,omitempty"`
		UpdatedAt time.Time `bson:"updatedAt,omitempty"`
	}
	sr := coll.FindOne(ctx, bson.M{"_id": oid})
	if err := sr.Decode(&doc); err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":   "read",
		"trace_id": getTraceIDFromContext(ctx),
		"endpoint": "MongoDB Read",
		"text":     doc.Text,
	})
}

func handleMessageMongoDelete(c *gin.Context) {
	var req MongoIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	oid, err := bson.ObjectIDFromHex(req.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	ctx := c.Request.Context()
	coll := mongoClient.Database(mongoDBName).Collection(mongoColl)
	res, err := coll.DeleteOne(ctx, bson.M{"_id": oid})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete"})
		return
	}
	if res.DeletedCount == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":   "deleted",
		"trace_id": getTraceIDFromContext(ctx),
		"endpoint": "MongoDB Delete",
	})
}
