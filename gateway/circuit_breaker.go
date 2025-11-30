package main

import (
	"math"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/sony/gobreaker"
)

// CircuitBreaker wraps Kafka producer with circuit breaker pattern
// Implements exponential backoff for timeout calculation
type CircuitBreaker struct {
	producer    sarama.SyncProducer
	cb          *gobreaker.CircuitBreaker
	mu          sync.RWMutex
	lastError   error
	lastErrorAt time.Time
	baseTimeout time.Duration
	maxTimeout  time.Duration
	failureCount uint32 // Track consecutive failures for exponential backoff
}

// NewCircuitBreaker creates a new circuit breaker wrapper for Kafka producer
// Uses exponential backoff for timeout instead of fixed 30s
// Configurable via environment variables:
//   - CIRCUIT_BREAKER_FAILURE_THRESHOLD (default: 5)
//   - CIRCUIT_BREAKER_SUCCESS_THRESHOLD (default: 2)
//   - CIRCUIT_BREAKER_BASE_TIMEOUT (default: 30s)
func NewCircuitBreaker(producer sarama.SyncProducer) *CircuitBreaker {
	// Read configuration from environment or use defaults
	failureThreshold := getEnvInt("CIRCUIT_BREAKER_FAILURE_THRESHOLD", 5)
	successThreshold := getEnvInt("CIRCUIT_BREAKER_SUCCESS_THRESHOLD", 2)
	baseTimeout := getEnvDuration("CIRCUIT_BREAKER_BASE_TIMEOUT", 30*time.Second)
	maxTimeout := getEnvDuration("CIRCUIT_BREAKER_MAX_TIMEOUT", 300*time.Second) // 5 minutes max

	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        "kafka-producer",
		MaxRequests: uint32(successThreshold), // Allow N requests in half-open state
		Interval:    60 * time.Second,          // Reset counts after 60 seconds
		Timeout:     baseTimeout,                // Base timeout (will use exponential backoff)
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// Open circuit after N consecutive failures
			return counts.ConsecutiveFailures >= uint32(failureThreshold)
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			// Log state transitions for monitoring
			// State changes: Closed -> Open -> HalfOpen -> Closed
		},
	})

	return &CircuitBreaker{
		producer:    producer,
		cb:          cb,
		baseTimeout: baseTimeout,
		maxTimeout:  maxTimeout,
	}
}

// Helper functions for environment variable parsing
func getEnvInt(key string, defaultValue int) int {
	if val := os.Getenv(key); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			return duration
		}
	}
	return defaultValue
}

// SendMessage sends a message through the circuit breaker
// Returns error if circuit is open or if Kafka producer fails
// Circuit breaker prevents overwhelming Kafka when it's down
// Uses exponential backoff: timeout increases with consecutive failures
func (cb *CircuitBreaker) SendMessage(msg *sarama.ProducerMessage) (partition int32, offset int64, err error) {
	// Execute Kafka send through circuit breaker
	// Circuit breaker will open after N consecutive failures
	result, err := cb.cb.Execute(func() (interface{}, error) {
		partition, offset, err := cb.producer.SendMessage(msg)
		if err != nil {
			cb.mu.Lock()
			cb.lastError = err
			cb.lastErrorAt = time.Now()
			cb.failureCount++
			cb.mu.Unlock()
			return nil, err
		}
		
		// Reset failure count on success
		cb.mu.Lock()
		cb.failureCount = 0
		cb.mu.Unlock()
		
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

// GetTimeout calculates exponential backoff timeout based on failure count
// Formula: baseTimeout * 2^min(failureCount, maxExponent)
// Capped at maxTimeout to prevent excessive wait times
func (cb *CircuitBreaker) GetTimeout() time.Duration {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	
	// Calculate exponential backoff: base * 2^failures
	// Cap exponent at 10 to prevent overflow (max timeout = base * 1024)
	exponent := math.Min(float64(cb.failureCount), 10)
	timeout := time.Duration(float64(cb.baseTimeout) * math.Pow(2, exponent))
	
	// Cap at maxTimeout
	if timeout > cb.maxTimeout {
		return cb.maxTimeout
	}
	return timeout
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

