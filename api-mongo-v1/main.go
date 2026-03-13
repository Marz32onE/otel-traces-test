// api-mongo-v1 runs the same MongoDB CRUD HTTP API as the main api service,
// but uses the otel-mongo v1 wrapper (root package) and go.mongodb.org/mongo-driver v1
// to verify trace propagation with the v1 driver in the demo project.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Marz32onE/instrumentation-go/otel-mongo/otelmongo"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const mongoDBName, mongoColl = "messaging", "messages"

var mongoClient *otelmongo.Client

func initOTEL(endpoint string, attrs ...attribute.KeyValue) (*sdktrace.TracerProvider, propagation.TextMapPropagator, error) {
	if endpoint == "" {
		endpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	}
	if endpoint == "" {
		endpoint = "localhost:4317"
	}
	useHTTP := strings.Contains(endpoint, "4318") || strings.HasPrefix(endpoint, "http")
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

func getTraceIDFromContext(ctx context.Context) string {
	if span := trace.SpanFromContext(ctx); span.SpanContext().HasTraceID() {
		return span.SpanContext().TraceID().String()
	}
	return ""
}

func main() {
	tp, _, err := initOTEL("", attribute.String("service.name", "api-mongo-v1"), attribute.String("service.version", "0.0.1"))
	if err != nil {
		log.Fatalf("initOTEL: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(ctx)
	}()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}
	ctx := context.Background()
	var errConn error
	mongoClient, errConn = otelmongo.NewClient(ctx, mongoURI, otelmongo.WithTracerProvider(tp), otelmongo.WithPropagators(otel.GetTextMapPropagator()))
	if errConn != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", errConn)
	}
	defer func() {
		_ = mongoClient.Disconnect(context.Background())
	}()
	if err := mongoClient.Ping(context.Background(), nil); err != nil {
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
	r.Use(otelgin.Middleware("api-mongo-v1"))

	r.POST("/api/message-mongo", handleMessageMongo)
	r.POST("/api/message-mongo-update", handleMessageMongoUpdate)
	r.POST("/api/message-mongo-read", handleMessageMongoRead)
	r.POST("/api/message-mongo-delete", handleMessageMongoDelete)
	r.POST("/api/message-mongo-bulk-insert", handleMessageMongoBulkInsert)
	r.POST("/api/message-mongo-bulk-update", handleMessageMongoBulkUpdate)

	log.Println("api-mongo-v1 server starting on :8089")
	if err := r.Run(":8089"); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

type MessageRequest struct {
	Text string `json:"text" binding:"required"`
}

type MongoIDRequest struct {
	ID string `json:"id" binding:"required"`
}

type MongoUpdateRequest struct {
	ID   string `json:"id" binding:"required"`
	Text string `json:"text"`
}

type MongoBulkInsertRequest struct {
	Texts []string `json:"texts" binding:"required,dive,min=1"`
}

type MongoBulkUpdateItem struct {
	ID   string `json:"id" binding:"required"`
	Text string `json:"text"`
}

type MongoBulkUpdateRequest struct {
	Updates []MongoBulkUpdateItem `json:"updates" binding:"required,dive"`
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
	if oid, ok := res.InsertedID.(primitive.ObjectID); ok {
		idHex = oid.Hex()
	}
	c.JSON(http.StatusOK, gin.H{
		"status":   "stored",
		"trace_id": getTraceIDFromContext(ctx),
		"endpoint": "MongoDB (v1)",
		"id":       idHex,
	})
}

func handleMessageMongoUpdate(c *gin.Context) {
	var req MongoUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	oid, err := primitive.ObjectIDFromHex(req.ID)
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
		"endpoint": "MongoDB Update (v1)",
	})
}

func handleMessageMongoRead(c *gin.Context) {
	var req MongoIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	oid, err := primitive.ObjectIDFromHex(req.ID)
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
		"endpoint": "MongoDB Read (v1)",
		"text":     doc.Text,
	})
}

func handleMessageMongoDelete(c *gin.Context) {
	var req MongoIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	oid, err := primitive.ObjectIDFromHex(req.ID)
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
		"endpoint": "MongoDB Delete (v1)",
	})
}

func handleMessageMongoBulkInsert(c *gin.Context) {
	var req MongoBulkInsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	coll := mongoClient.Database(mongoDBName).Collection(mongoColl)
	models := make([]mongo.WriteModel, 0, len(req.Texts))
	for _, text := range req.Texts {
		doc := bson.M{"text": text, "createdAt": time.Now()}
		models = append(models, mongo.NewInsertOneModel().SetDocument(doc))
	}
	res, err := coll.BulkWrite(ctx, models)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to bulk insert"})
		return
	}
	// v1 driver BulkWriteResult has InsertedCount but no InsertedIDs slice; return count only.
	c.JSON(http.StatusOK, gin.H{
		"status":   "bulk_stored",
		"trace_id": getTraceIDFromContext(ctx),
		"endpoint": "MongoDB Bulk Insert (v1)",
		"count":    res.InsertedCount,
	})
}

func handleMessageMongoBulkUpdate(c *gin.Context) {
	var req MongoBulkUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	coll := mongoClient.Database(mongoDBName).Collection(mongoColl)
	models := make([]mongo.WriteModel, 0, len(req.Updates))
	for _, u := range req.Updates {
		oid, err := primitive.ObjectIDFromHex(u.ID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id: " + u.ID})
			return
		}
		update := bson.M{"$set": bson.M{"text": u.Text, "updatedAt": time.Now()}}
		models = append(models, mongo.NewUpdateOneModel().SetFilter(bson.M{"_id": oid}).SetUpdate(update))
	}
	res, err := coll.BulkWrite(ctx, models)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to bulk update"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":          "bulk_updated",
		"trace_id":        getTraceIDFromContext(ctx),
		"endpoint":        "MongoDB Bulk Update (v1)",
		"modified_count":  res.ModifiedCount,
		"matched_count":   res.MatchedCount,
	})
}
