package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/yourname/flash-sale-engine/common"
)

var (
	redisClient    *redis.Client
	producer       *CircuitBreaker
	logger         *logrus.Logger
	ctx            = context.Background()
)

type OrderRequest struct {
	UserID    string `json:"user_id"`
	ItemID    string `json:"item_id"`
	Amount    int    `json:"amount"`
	RequestID string `json:"request_id"` // Unique request identifier for idempotency checks
}

func main() {
	// Initialize structured logger
	logger = common.InitLogger()
	logger.Info("Gateway starting...")

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

	// Test Redis connection
	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.WithError(err).Fatal("Failed to connect to Redis")
	}
	logger.Info("Connected to Redis")

	// 2. Connect to Kafka with Circuit Breaker
	config := sarama.NewConfig()
	config.Producer.Return.Successes = true
	rawProducer, err := sarama.NewSyncProducer([]string{kafkaAddr}, config)
	if err != nil {
		logger.WithError(err).Fatal("Failed to start Kafka producer")
	}

	// Wrap producer with circuit breaker
	producer = NewCircuitBreaker(rawProducer)
	logger.Info("Kafka producer initialized with circuit breaker")

	http.HandleFunc("/buy", handleBuy)
	http.HandleFunc("/health", handleHealth)

	logger.Info("Gateway running on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		logger.WithError(err).Fatal("HTTP server failed")
	}
}

func handleBuy(w http.ResponseWriter, r *http.Request) {
	// Generate correlation ID for request tracing
	correlationID := uuid.New().String()
	logEntry := common.WithCorrelationID(correlationID)
	logEntry.Info("Received buy request")

	// Set content type for JSON responses
	w.Header().Set("Content-Type", "application/json")

	// Decode request body
	var order OrderRequest
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		logEntry.WithError(err).Warn("Invalid request body")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid request body",
			"correlation_id": correlationID,
		})
		return
	}

	// Validate input fields (user_id, item_id, amount, request_id)
	// Returns 400 Bad Request with detailed error messages if validation fails
	if validationErrors := ValidateOrderRequest(&order); len(validationErrors) > 0 {
		logEntry.WithField("errors", validationErrors).Warn("Validation failed")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Validation failed",
			"errors": validationErrors,
			"correlation_id": correlationID,
		})
		return
	}

	logEntry = logEntry.WithFields(map[string]interface{}{
		"user_id":    order.UserID,
		"item_id":    order.ItemID,
		"amount":     order.Amount,
		"request_id": order.RequestID,
	})

	// Idempotency check: Use Redis SETNX to prevent duplicate order processing
	// If request_id already exists, return 409 Conflict
	// TTL of 10 minutes ensures idempotency keys don't accumulate indefinitely
	isNew, err := redisClient.SetNX(ctx, "idempotency:"+order.RequestID, "processing", 10*time.Minute).Result()
	if err != nil {
		logEntry.WithError(err).Error("Redis idempotency check failed")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Internal server error",
			"correlation_id": correlationID,
		})
		return
	}
	if !isNew {
		logEntry.Warn("Duplicate request detected")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Duplicate Request Detected",
			"correlation_id": correlationID,
		})
		return
	}

	// Publish order to Kafka for async processing
	// Include correlation ID in message headers for request tracing across services
	orderBytes, _ := json.Marshal(order)
	msg := &sarama.ProducerMessage{
		Topic: "orders",
		Value: sarama.StringEncoder(orderBytes),
		Headers: []sarama.RecordHeader{
			{Key: []byte("correlation_id"), Value: []byte(correlationID)},
		},
	}

	// Check circuit breaker state before attempting to send
	// If circuit is open, Kafka is unavailable - return 503 and rollback idempotency key
	cbState := producer.State()
	if cbState.String() == "Open" {
		logEntry.WithField("circuit_state", cbState.String()).Error("Circuit breaker is open")
		// Rollback idempotency key since we're not processing this request
		redisClient.Del(ctx, "idempotency:"+order.RequestID)
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Service temporarily unavailable",
			"correlation_id": correlationID,
		})
		return
	}

	// Send message through circuit breaker (handles failures gracefully)
	_, _, err = producer.SendMessage(msg)
	if err != nil {
		logEntry.WithError(err).WithField("circuit_state", producer.State().String()).Error("Failed to send message to Kafka")
		// Rollback idempotency key since message wasn't queued
		redisClient.Del(ctx, "idempotency:"+order.RequestID)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Failed to queue order",
			"correlation_id": correlationID,
		})
		return
	}

	logEntry.Info("Order queued successfully")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "Order Queued",
		"correlation_id": correlationID,
	})
}

// handleHealth provides a health check endpoint for Kubernetes liveness/readiness probes
// Returns 200 OK if all services are healthy, 503 Service Unavailable otherwise
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	// Check Redis connection health
	ctx := context.Background()
	redisHealthy := true
	if err := redisClient.Ping(ctx).Err(); err != nil {
		redisHealthy = false
	}

	// Check Kafka health via circuit breaker state
	// Circuit breaker open indicates Kafka is unavailable
	kafkaHealthy := producer.State().String() != "Open"

	status := http.StatusOK
	if !redisHealthy || !kafkaHealthy {
		status = http.StatusServiceUnavailable
	}

	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"redis":  redisHealthy,
		"kafka":  kafkaHealthy,
		"circuit_breaker_state": producer.State().String(),
	})
}

