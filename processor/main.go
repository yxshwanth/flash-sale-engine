package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/IBM/sarama"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/yourname/flash-sale-engine/common"
)

var (
	redisClient          *redis.Client
	producer             sarama.SyncProducer // Kafka producer for publishing failed orders to DLQ
	ctx                  = context.Background()
	logger               *logrus.Logger
	metrics              *common.ProcessorMetrics
	checkInventoryScript *redis.Script
)

type OrderRequest struct {
	UserID string `json:"user_id"`
	ItemID string `json:"item_id"`
}

func main() {
	// Initialize structured logger with service name
	logger = common.InitLogger("processor")
	logger.Info("Processor starting...")

	// Get service addresses from environment or use defaults
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "redis-service:6379" // Default for k8s
	}

	kafkaAddr := os.Getenv("KAFKA_ADDR")
	if kafkaAddr == "" {
		kafkaAddr = "kafka-service:9092" // Default for k8s
	}

	redisClient = redis.NewClient(&redis.Options{Addr: redisAddr})

	// Load Lua scripts
	checkInventoryScript = redis.NewScript(luaCheckInventoryScript)

	// Setup DLQ Producer
	config := sarama.NewConfig()
	config.Producer.Return.Successes = true
	var err error
	producer, err = sarama.NewSyncProducer([]string{kafkaAddr}, config)
	if err != nil {
		logger.WithError(err).Fatal("DLQ Producer failed")
	}

	// Consumer Setup
	consumer, err := sarama.NewConsumer([]string{kafkaAddr}, nil)
	if err != nil {
		logger.WithError(err).Fatal("Consumer failed")
	}

	partitionConsumer, err := consumer.ConsumePartition("orders", 0, sarama.OffsetNewest)
	if err != nil {
		logger.WithError(err).Fatal("Partition failed")
	}

	// Initialize Prometheus metrics
	metrics = common.InitProcessorMetrics()

	// Start metrics HTTP server for Prometheus scraping
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(":9090", nil); err != nil {
			logger.WithError(err).Error("Metrics server failed")
		}
	}()

	logger.Info("Processor started and ready to process orders")

	// Setup graceful shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Process messages in goroutine
	done := make(chan bool)
	go func() {
		for msg := range partitionConsumer.Messages() {
			processOrder(msg)
		}
		done <- true
	}()

	// Wait for shutdown signal or consumer to stop
	select {
	case <-shutdown:
		logger.Info("Shutdown signal received, draining in-flight orders...")

		// Close consumer (stops receiving new messages)
		if err := partitionConsumer.Close(); err != nil {
			logger.WithError(err).Error("Error closing partition consumer")
		}
		if err := consumer.Close(); err != nil {
			logger.WithError(err).Error("Error closing consumer")
		}

		// Wait for current message processing to complete (with timeout)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		select {
		case <-done:
			logger.Info("All orders processed")
		case <-shutdownCtx.Done():
			logger.Warn("Shutdown timeout reached, some orders may not be processed")
		}

		// Close connections
		if err := producer.Close(); err != nil {
			logger.WithError(err).Error("Error closing DLQ producer")
		}
		if err := redisClient.Close(); err != nil {
			logger.WithError(err).Error("Error closing Redis client")
		}

		logger.Info("Processor shutdown complete")
	case <-done:
		logger.Info("Consumer stopped")
	}
}

func processOrder(msg *sarama.ConsumerMessage) {
	// Track processing time
	startTime := time.Now()

	// Extract correlation ID from Kafka headers
	correlationID := extractCorrelationID(msg.Headers)
	logEntry := common.WithEvent(correlationID, "order_processing_started")

	var order OrderRequest
	if err := json.Unmarshal(msg.Value, &order); err != nil {
		logEntry.WithError(err).WithField("event", "order_unmarshal_failed").Error("Failed to unmarshal order")
		moveToDLQ(msg, "Invalid Order Format", correlationID)
		return
	}

	logEntry = logEntry.WithFields(map[string]interface{}{
		"user_id":            order.UserID,
		"item_id":            order.ItemID,
		"message_size_bytes": len(msg.Value),
		"kafka_offset":       msg.Offset,
		"kafka_partition":    msg.Partition,
	})

	logEntry.Info("Processing order")

	// Track order processing
	metrics.OrdersProcessed.Inc()

	// Atomic inventory check using Redis Lua script
	// Lua script ensures DECR and conditional INCR (refund) are atomic
	// This prevents race conditions where inventory could go negative
	// Edge cases handled: missing keys, Redis OOM, timeouts
	inventoryKey := "inventory:" + order.ItemID

	// Add timeout context for script execution (5 seconds)
	// Prevents hanging if Redis is slow or unresponsive
	scriptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	result, err := checkInventoryScript.Run(scriptCtx, redisClient, []string{inventoryKey}).Result()

	if err != nil {
		// Handle Redis errors (OOM, timeout, connection issues)
		if err == context.DeadlineExceeded {
			logEntry.WithError(err).Error("Redis script execution timeout")
			moveToDLQ(msg, "Redis Timeout", correlationID)
		} else {
			logEntry.WithError(err).Error("Redis script execution failed")
			moveToDLQ(msg, "Redis Failure", correlationID)
		}
		return
	}

	// Parse Lua script result: {success: 0|1, stock: int, reason: string}
	// success=0 means sold out or not initialized (already refunded by script)
	// success=1 means inventory reserved successfully
	results := result.([]interface{})
	success := results[0].(int64)
	stock := results[1].(int64)
	reason := "UNKNOWN"
	if len(results) > 2 {
		// Handle both string and []byte types from Redis
		switch v := results[2].(type) {
		case string:
			reason = v
		case []byte:
			reason = string(v)
		default:
			reason = "UNKNOWN"
		}
	}

	if success == 0 {
		// Item sold out or not initialized - Lua script already handled refund
		metrics.OrdersSoldOut.Inc()
		metrics.OrdersProcessedFailed.Inc()
		logEntry.WithFields(map[string]interface{}{
			"stock":  stock,
			"reason": reason,
			"event":  "order_sold_out",
		}).Warn("Order failed: Item unavailable")
		return
	}

	// Update inventory level metric
	metrics.InventoryLevels.WithLabelValues(order.ItemID).Set(float64(stock))

	logEntry.WithField("stock_after", stock).Info("Inventory reserved successfully")

	// Simulate payment processing (in production, this would call payment service)
	// For demonstration: 10% of orders fail to simulate payment service timeouts
	if time.Now().Unix()%10 == 0 {
		logEntry.Warn("Payment Service Timeout! Moving to DLQ.")

		// Refund inventory atomically using Lua script
		// Ensures inventory is restored even if refund operation is interrupted
		refundScript := redis.NewScript(luaRefundInventoryScript)
		refundCtx, refundCancel := context.WithTimeout(ctx, 5*time.Second)
		defer refundCancel()

		refundResult, refundErr := refundScript.Run(refundCtx, redisClient, []string{inventoryKey}, 1).Result()
		if refundErr != nil {
			if refundErr == context.DeadlineExceeded {
				logEntry.WithError(refundErr).Error("Inventory refund timeout")
			} else {
				logEntry.WithError(refundErr).Error("Failed to refund inventory")
			}
		} else {
			// Parse refund result: {success: 0|1, new_stock: int}
			if refundResult != nil {
				refundResults := refundResult.([]interface{})
				if len(refundResults) >= 2 {
					newStock := refundResults[1].(int64)
					logEntry.WithField("new_stock", newStock).Info("Inventory refunded successfully")
				}
			}
		}

		// Move failed order to Dead Letter Queue for manual review/retry
		moveToDLQ(msg, "Payment Timeout", correlationID)
		return
	}

	// Log success with processing time
	processingTime := time.Since(startTime)
	logEntry.WithFields(map[string]interface{}{
		"event":              "order_processed_success",
		"processing_time_ms": processingTime.Milliseconds(),
	}).Info("Order processed successfully")
}

// extractCorrelationID extracts correlation ID from Kafka message headers
// If not found, generates a new one for processor-originated logs
// This ensures all logs can be traced even if correlation ID wasn't propagated
func extractCorrelationID(headers []*sarama.RecordHeader) string {
	for _, header := range headers {
		if string(header.Key) == "correlation_id" {
			return string(header.Value)
		}
	}
	// Generate processor-specific correlation ID if not found in headers
	return "proc-" + strconv.FormatInt(time.Now().UnixNano(), 10)
}

// extractRequestID extracts request ID from Kafka message headers
// Used for order status tracking
func extractRequestID(headers []*sarama.RecordHeader) string {
	for _, header := range headers {
		if string(header.Key) == "request_id" {
			return string(header.Value)
		}
	}
	return ""
}

func moveToDLQ(msg *sarama.ConsumerMessage, reason string, correlationID string) {
	// Record DLQ metrics
	RecordFailure(reason)

	dlqMsg := &sarama.ProducerMessage{
		Topic: "orders-dlq",
		Value: sarama.ByteEncoder(msg.Value),
		Headers: []sarama.RecordHeader{
			{Key: []byte("error"), Value: []byte(reason)},
			{Key: []byte("correlation_id"), Value: []byte(correlationID)},
			{Key: []byte("timestamp"), Value: []byte(time.Now().Format(time.RFC3339))},
		},
	}

	_, _, err := producer.SendMessage(dlqMsg)
	if err != nil {
		common.WithCorrelationID(correlationID).
			WithError(err).
			WithField("event", "dlq_send_failed").
			Error("Failed to send message to DLQ")
		return
	}

	common.WithCorrelationID(correlationID).
		WithFields(map[string]interface{}{
			"reason": reason,
			"event":  "message_moved_to_dlq",
		}).
		Warn("Message moved to DLQ")
}
