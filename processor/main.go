package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/IBM/sarama"
	"github.com/redis/go-redis/v9"
)

var (
	redisClient *redis.Client
	producer    sarama.SyncProducer // To publish to DLQ
	ctx         = context.Background()
)

type OrderRequest struct {
	UserID string `json:"user_id"`
	ItemID string `json:"item_id"`
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

	redisClient = redis.NewClient(&redis.Options{Addr: redisAddr})

	// Setup DLQ Producer
	config := sarama.NewConfig()
	config.Producer.Return.Successes = true
	var err error
	producer, err = sarama.NewSyncProducer([]string{kafkaAddr}, config)
	if err != nil {
		log.Fatal("DLQ Producer failed:", err)
	}

	// Consumer Setup
	consumer, err := sarama.NewConsumer([]string{kafkaAddr}, nil)
	if err != nil {
		log.Fatal("Consumer failed:", err)
	}

	partitionConsumer, err := consumer.ConsumePartition("orders", 0, sarama.OffsetNewest)
	if err != nil {
		log.Fatal("Partition failed:", err)
	}

	log.Println("Processor started...")

	for msg := range partitionConsumer.Messages() {
		processOrder(msg)
	}
}

func processOrder(msg *sarama.ConsumerMessage) {
	var order OrderRequest
	json.Unmarshal(msg.Value, &order)

	// --- RESUME FEATURE: ATOMIC INVENTORY CHECK ---
	// DECR decrements the value by 1 and returns the new value.
	// This is atomic - race conditions are impossible here.
	stock, err := redisClient.Decr(ctx, "inventory:"+order.ItemID).Result()
	
	if err != nil {
		moveToDLQ(msg, "Redis Failure")
		return
	}

	if stock < 0 {
		// Sold out! Revert the decrement to keep data clean (optional but good practice)
		redisClient.Incr(ctx, "inventory:"+order.ItemID)
		log.Printf("Order failed: Item %s is sold out.", order.ItemID)
		return
	}

	// Simulate Payment Processing...
	if time.Now().Unix()%10 == 0 { 
		// --- RESUME FEATURE: FAULT TOLERANCE DEMO ---
		// Simulate a random crash for 10% of requests
		log.Printf("Payment Service Timeout! Moving to DLQ.")
		redisClient.Incr(ctx, "inventory:"+order.ItemID) // Refund inventory
		moveToDLQ(msg, "Payment Timeout")
		return
	}

	log.Printf("Order Processed Successfully for User %s", order.UserID)
}

func moveToDLQ(msg *sarama.ConsumerMessage, reason string) {
	dlqMsg := &sarama.ProducerMessage{
		Topic: "orders-dlq",
		Value: sarama.ByteEncoder(msg.Value),
		Headers: []sarama.RecordHeader{
			{Key: []byte("error"), Value: []byte(reason)},
		},
	}
	producer.SendMessage(dlqMsg)
	log.Println("Message moved to DLQ")
}

