package main

import (
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/sony/gobreaker"
)

// CircuitBreaker wraps Kafka producer with circuit breaker pattern
type CircuitBreaker struct {
	producer    sarama.SyncProducer
	cb          *gobreaker.CircuitBreaker
	mu          sync.RWMutex
	lastError   error
	lastErrorAt time.Time
}

// NewCircuitBreaker creates a new circuit breaker wrapper for Kafka producer
func NewCircuitBreaker(producer sarama.SyncProducer) *CircuitBreaker {
	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        "kafka-producer",
		MaxRequests: 3,              // Allow 3 requests in half-open state
		Interval:    60 * time.Second, // Reset counts after 60 seconds
		Timeout:     30 * time.Second, // Timeout before trying again
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// Open circuit after 5 consecutive failures to prevent cascading failures
			// This threshold balances between quick failure detection and avoiding false positives
			return counts.ConsecutiveFailures >= 5
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			// Circuit breaker state change callback
			// Can be enhanced with structured logging to track state transitions
		},
	})

	return &CircuitBreaker{
		producer: producer,
		cb:       cb,
	}
}

// SendMessage sends a message through the circuit breaker
// Returns error if circuit is open or if Kafka producer fails
// Circuit breaker prevents overwhelming Kafka when it's down
func (cb *CircuitBreaker) SendMessage(msg *sarama.ProducerMessage) (partition int32, offset int64, err error) {
	// Execute Kafka send through circuit breaker
	// Circuit breaker will open after 5 consecutive failures
	result, err := cb.cb.Execute(func() (interface{}, error) {
		partition, offset, err := cb.producer.SendMessage(msg)
		if err != nil {
			cb.mu.Lock()
			cb.lastError = err
			cb.lastErrorAt = time.Now()
			cb.mu.Unlock()
			return nil, err
		}
		return map[string]interface{}{
			"partition": partition,
			"offset":    offset,
		}, nil
	})

	if err != nil {
		// Circuit breaker is open (Kafka unavailable) or execution failed
		return 0, 0, err
	}

	// Extract partition and offset from successful result
	if result != nil {
		res := result.(map[string]interface{})
		return res["partition"].(int32), res["offset"].(int64), nil
	}

	return 0, 0, nil
}

// State returns the current circuit breaker state
func (cb *CircuitBreaker) State() gobreaker.State {
	return cb.cb.State()
}

// LastError returns the last error that occurred
func (cb *CircuitBreaker) LastError() error {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.lastError
}

// Close closes the underlying producer
func (cb *CircuitBreaker) Close() error {
	return cb.producer.Close()
}

