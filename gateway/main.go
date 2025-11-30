package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/IBM/sarama"
	"github.com/redis/go-redis/v9"
)

var (
	redisClient *redis.Client
	producer    sarama.SyncProducer
	ctx         = context.Background()
)

type OrderRequest struct {
	UserID    string `json:"user_id"`
	ItemID    string `json:"item_id"`
	Amount    int    `json:"amount"`
	RequestID string `json:"request_id"` // Used for Idempotency
}

func main() {
	// Get service addresses from environment or use defaults
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "redis-service:6379" // Default for k8s
	}

	kafkaAddr := os.Getenv("KAFKA_ADDR")
	if kafkaAddr == "" {
		kafkaAddr = "kafka-service:9092" // Default for k8s
	}

	// 1. Connect to Redis
	redisClient = redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	// 2. Connect to Kafka
	config := sarama.NewConfig()
	config.Producer.Return.Successes = true
	var err error
	producer, err = sarama.NewSyncProducer([]string{kafkaAddr}, config)
	if err != nil {
		log.Fatal("Failed to start Kafka producer:", err)
	}

	http.HandleFunc("/buy", handleBuy)
	log.Println("Gateway running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleBuy(w http.ResponseWriter, r *http.Request) {
	var order OrderRequest
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		http.Error(w, "Invalid body", http.StatusBadRequest)
		return
	}

	// --- RESUME FEATURE: IDEMPOTENCY ---
	// SETNX (Set if Not Exists) prevents double processing
	isNew, err := redisClient.SetNX(ctx, "idempotency:"+order.RequestID, "processing", 10*time.Minute).Result()
	if err != nil {
		http.Error(w, "Redis error", http.StatusInternalServerError)
		return
	}
	if !isNew {
		http.Error(w, "Duplicate Request Detected", http.StatusConflict)
		return
	}

	// --- RESUME FEATURE: ASYNC PROCESSING ---
	orderBytes, _ := json.Marshal(order)
	msg := &sarama.ProducerMessage{
		Topic: "orders",
		Value: sarama.StringEncoder(orderBytes),
	}

	_, _, err = producer.SendMessage(msg)
	if err != nil {
		// Rollback Redis if Kafka fails
		redisClient.Del(ctx, "idempotency:"+order.RequestID)
		http.Error(w, "Failed to queue order", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status": "Order Queued"}`))
}

