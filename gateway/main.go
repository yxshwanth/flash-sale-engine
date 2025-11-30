package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/yourname/flash-sale-engine/common"
)

var (
	redisClient *redis.Client
	producer    *CircuitBreaker
	rateLimiter *RateLimiter
	logger      *logrus.Logger
	metrics     *common.GatewayMetrics
	ctx         = context.Background()
)

type OrderRequest struct {
	UserID    string `json:"user_id"`
	ItemID    string `json:"item_id"`
	Amount    int    `json:"amount"`
	RequestID string `json:"request_id"` // Unique request identifier for idempotency checks
}

func main() {
	// Initialize structured logger with service name
	logger = common.InitLogger("gateway")
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

	// Initialize rate limiter
	// Configurable via environment: RATE_LIMIT_MAX_REQUESTS (default: 60), RATE_LIMIT_WINDOW (default: 1m)
	maxRequests := getEnvInt("RATE_LIMIT_MAX_REQUESTS", 60)
	windowSize := getEnvDuration("RATE_LIMIT_WINDOW", 1*time.Minute)
	rateLimiter = NewRateLimiter(redisClient, maxRequests, windowSize)
	logger.WithFields(map[string]interface{}{
		"max_requests": maxRequests,
		"window_size":  windowSize.String(),
	}).Info("Rate limiter initialized")

	// Initialize Prometheus metrics
	metrics = common.InitGatewayMetrics()

	http.HandleFunc("/buy", handleBuy)
	http.HandleFunc("/health", handleHealth)
	http.Handle("/metrics", promhttp.Handler()) // Prometheus metrics endpoint

	// Setup graceful shutdown
	server := &http.Server{
		Addr:    ":8080",
		Handler: nil,
	}

	// Channel to listen for interrupt signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Start server in goroutine
	go func() {
		logger.Info("Gateway running on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Fatal("HTTP server failed")
		}
	}()

	// Wait for shutdown signal
	<-shutdown
	logger.Info("Shutdown signal received, draining connections...")

	// Create shutdown context with timeout (30 seconds to drain)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Gracefully shutdown server (stops accepting new connections, waits for existing)
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.WithError(err).Error("Error during server shutdown")
	}

	// Close connections
	if err := producer.Close(); err != nil {
		logger.WithError(err).Error("Error closing Kafka producer")
	}
	if err := redisClient.Close(); err != nil {
		logger.WithError(err).Error("Error closing Redis client")
	}

	logger.Info("Gateway shutdown complete")
}

func handleBuy(w http.ResponseWriter, r *http.Request) {
	// Add request timeout context (30 seconds)
	reqCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Track processing time for metrics
	startTime := time.Now()

	// Generate correlation ID for request tracing
	correlationID := uuid.New().String()
	logEntry := common.WithEvent(correlationID, "order_received")

	// Log request details
	logEntry.WithFields(map[string]interface{}{
		"method":      r.Method,
		"path":        r.URL.Path,
		"remote_addr": r.RemoteAddr,
		"user_agent":  r.UserAgent(),
	}).Info("Received buy request")

	// Set content type for JSON responses
	w.Header().Set("Content-Type", "application/json")

	// Decode request body
	var order OrderRequest
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		logEntry.WithError(err).Warn("Invalid request body")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":          "Invalid request body",
			"correlation_id": correlationID,
		})
		return
	}

	// Track order received
	metrics.OrdersReceived.Inc()

	// Rate limiting: Check if user has exceeded rate limit
	// Use request context with timeout
	allowed, err := rateLimiter.Allow(reqCtx, order.UserID)
	if err != nil {
		// Redis error - log but allow request (fail open)
		logEntry.WithError(err).Warn("Rate limiter check failed, allowing request")
	} else if !allowed {
		metrics.OrdersFailed.Inc()
		logEntry.WithField("event", "rate_limit_exceeded").Warn("Rate limit exceeded")
		w.WriteHeader(http.StatusTooManyRequests)
		remaining, _ := rateLimiter.GetRemainingRequests(reqCtx, order.UserID)
		rateLimitWindowDuration := getEnvDuration("RATE_LIMIT_WINDOW", 1*time.Minute)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":               "Rate limit exceeded",
			"correlation_id":      correlationID,
			"retry_after_seconds": int(rateLimitWindowDuration.Seconds()),
			"remaining_requests":  remaining,
		})
		return
	}

	// Validate input fields (user_id, item_id, amount, request_id)
	// Returns 400 Bad Request with detailed error messages if validation fails
	if validationErrors := ValidateOrderRequest(&order); len(validationErrors) > 0 {
		metrics.OrdersValidationFailed.Inc()
		logEntry.WithField("errors", validationErrors).Warn("Validation failed")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":          "Validation failed",
			"errors":         validationErrors,
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
	// Use request context with timeout
	isNew, err := redisClient.SetNX(reqCtx, "idempotency:"+order.RequestID, "processing", 10*time.Minute).Result()
	if err != nil {
		logEntry.WithError(err).Error("Redis idempotency check failed")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error":          "Internal server error",
			"correlation_id": correlationID,
		})
		return
	}
	if !isNew {
		metrics.OrdersIdempotencyRejected.Inc()
		logEntry.Warn("Duplicate request detected")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{
			"error":          "Duplicate Request Detected",
			"correlation_id": correlationID,
		})
		return
	}

	// Update order status to PROCESSING when queued
	orderStatusKey := "order_status:" + order.RequestID
	redisClient.Set(reqCtx, orderStatusKey, "PROCESSING", 30*time.Minute)

	// Publish order to Kafka for async processing
	// Include correlation ID in message headers for request tracing across services
	orderBytes, _ := json.Marshal(order)
	msg := &sarama.ProducerMessage{
		Topic: "orders",
		Value: sarama.StringEncoder(orderBytes),
		Headers: []sarama.RecordHeader{
			{Key: []byte("correlation_id"), Value: []byte(correlationID)},
			{Key: []byte("request_id"), Value: []byte(order.RequestID)},
		},
	}

	// Check circuit breaker state before attempting to send
	// If circuit is open, Kafka is unavailable - return 503 and rollback idempotency key
	cbState := producer.State()
	if cbState.String() == "Open" {
		logEntry.WithField("circuit_state", cbState.String()).Error("Circuit breaker is open")
		// Rollback idempotency key since we're not processing this request
		redisClient.Del(reqCtx, "idempotency:"+order.RequestID)
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error":          "Service temporarily unavailable",
			"correlation_id": correlationID,
		})
		return
	}

	// Send message through circuit breaker (handles failures gracefully)
	_, _, err = producer.SendMessage(msg)
	if err != nil {
		metrics.OrdersFailed.Inc()
		logEntry.WithError(err).WithField("circuit_state", producer.State().String()).Error("Failed to send message to Kafka")
		// Rollback idempotency key since message wasn't queued
		redisClient.Del(reqCtx, "idempotency:"+order.RequestID)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error":          "Failed to queue order",
			"correlation_id": correlationID,
		})
		return
	}

	// Record metrics
	processingTime := time.Since(startTime)
	metrics.OrdersSuccessful.Inc()
	metrics.RequestDuration.Observe(processingTime.Seconds())

	// Update circuit breaker state metric (0=closed, 1=open, 2=half-open)
	cbState = producer.State()
	stateValue := 0.0
	if cbState.String() == "Open" {
		stateValue = 1.0
	} else if cbState.String() == "HalfOpen" {
		stateValue = 2.0
	}
	metrics.CircuitBreakerState.Set(stateValue)

	// Log success with processing time
	logEntry.WithFields(map[string]interface{}{
		"processing_time_ms": processingTime.Milliseconds(),
		"event":              "order_queued",
	}).Info("Order queued successfully")

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":             "Order Queued",
		"correlation_id":     correlationID,
		"processing_time_ms": processingTime.Milliseconds(),
	})
}

// handleHealth provides a health check endpoint for Kubernetes liveness/readiness probes
// Returns 200 OK if all services are healthy, 503 Service Unavailable otherwise
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Check Redis connection health with timeout
	healthCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	redisHealthy := true
	if err := redisClient.Ping(healthCtx).Err(); err != nil {
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
		"status":                "healthy",
		"redis":                 redisHealthy,
		"kafka":                 kafkaHealthy,
		"circuit_breaker_state": producer.State().String(),
	})
}
