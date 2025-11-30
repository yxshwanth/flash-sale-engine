package main

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"time"

	"github.com/IBM/sarama"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/yourname/flash-sale-engine/common"
)

var (
	redisClient          *redis.Client
	producer             sarama.SyncProducer // Kafka producer for publishing failed orders to DLQ
	ctx                  = context.Background()
	logger               *logrus.Logger
	checkInventoryScript *redis.Script
)

type OrderRequest struct {
	UserID string `json:"user_id"`
	ItemID string `json:"item_id"`
}

func main() {
	// Initialize structured logger
	logger = common.InitLogger()
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

	logger.Info("Processor started and ready to process orders")

	for msg := range partitionConsumer.Messages() {
		processOrder(msg)
	}
}

func processOrder(msg *sarama.ConsumerMessage) {
	// Extract correlation ID from Kafka headers
	correlationID := extractCorrelationID(msg.Headers)
	logEntry := common.WithCorrelationID(correlationID)

	var order OrderRequest
	if err := json.Unmarshal(msg.Value, &order); err != nil {
		logEntry.WithError(err).Error("Failed to unmarshal order")
		moveToDLQ(msg, "Invalid Order Format", correlationID)
		return
	}

	logEntry = logEntry.WithFields(map[string]interface{}{
		"user_id": order.UserID,
		"item_id": order.ItemID,
	})

	logEntry.Info("Processing order")

	// Atomic inventory check using Redis Lua script
	// Lua script ensures DECR and conditional INCR (refund) are atomic
	// This prevents race conditions where inventory could go negative
	inventoryKey := "inventory:" + order.ItemID
	result, err := checkInventoryScript.Run(ctx, redisClient, []string{inventoryKey}).Result()

	if err != nil {
		logEntry.WithError(err).Error("Redis script execution failed")
		moveToDLQ(msg, "Redis Failure", correlationID)
		return
	}

	// Parse Lua script result: {success: 0|1, stock: int}
	// success=0 means sold out (inventory was negative, already refunded by script)
	// success=1 means inventory reserved successfully
	results := result.([]interface{})
	success := results[0].(int64)
	stock := results[1].(int64)

	if success == 0 {
		// Item sold out - Lua script already refunded the decrement
		logEntry.WithField("stock", stock).Warn("Order failed: Item sold out")
		return
	}

	logEntry.WithField("stock_after", stock).Info("Inventory reserved successfully")

	// Simulate payment processing (in production, this would call payment service)
	// For demonstration: 10% of orders fail to simulate payment service timeouts
	if time.Now().Unix()%10 == 0 {
		logEntry.Warn("Payment Service Timeout! Moving to DLQ.")

		// Refund inventory atomically using Lua script
		// Ensures inventory is restored even if refund operation is interrupted
		refundScript := redis.NewScript(luaRefundInventoryScript)
		_, refundErr := refundScript.Run(ctx, redisClient, []string{inventoryKey}, 1).Result()
		if refundErr != nil {
			logEntry.WithError(refundErr).Error("Failed to refund inventory")
		} else {
			logEntry.Info("Inventory refunded successfully")
		}

		// Move failed order to Dead Letter Queue for manual review/retry
		moveToDLQ(msg, "Payment Timeout", correlationID)
		return
	}

	logEntry.Info("Order processed successfully")
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

func moveToDLQ(msg *sarama.ConsumerMessage, reason string, correlationID string) {
	dlqMsg := &sarama.ProducerMessage{
		Topic: "orders-dlq",
		Value: sarama.ByteEncoder(msg.Value),
		Headers: []sarama.RecordHeader{
			{Key: []byte("error"), Value: []byte(reason)},
			{Key: []byte("correlation_id"), Value: []byte(correlationID)},
		},
	}

	_, _, err := producer.SendMessage(dlqMsg)
	if err != nil {
		common.WithCorrelationID(correlationID).WithError(err).Error("Failed to send message to DLQ")
		return
	}

	common.WithCorrelationID(correlationID).
		WithField("reason", reason).
		Info("Message moved to DLQ")
}
