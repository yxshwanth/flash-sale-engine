package main

import (
	"sync"
	"time"
)

// DLQMetrics tracks Dead Letter Queue statistics
type DLQMetrics struct {
	mu               sync.RWMutex
	totalFailures    int64
	failuresByReason map[string]int64
	oldestMessageAge time.Duration
	lastFailureTime  time.Time
}

var dlqMetrics = &DLQMetrics{
	failuresByReason: make(map[string]int64),
}

// RecordFailure records a failed order moved to DLQ
func RecordFailure(reason string) {
	dlqMetrics.mu.Lock()
	defer dlqMetrics.mu.Unlock()

	dlqMetrics.totalFailures++
	dlqMetrics.failuresByReason[reason]++
	dlqMetrics.lastFailureTime = time.Now()
}

// GetMetrics returns current DLQ metrics
func GetDLQMetrics() (totalFailures int64, failuresByReason map[string]int64, oldestAge time.Duration, lastFailure time.Time) {
	dlqMetrics.mu.RLock()
	defer dlqMetrics.mu.RUnlock()

	// Create a copy of failuresByReason to avoid race conditions
	reasonCopy := make(map[string]int64)
	for k, v := range dlqMetrics.failuresByReason {
		reasonCopy[k] = v
	}

	// Calculate oldest message age (simplified - in production, track per message)
	oldestAge = time.Since(dlqMetrics.lastFailureTime)
	if dlqMetrics.lastFailureTime.IsZero() {
		oldestAge = 0
	}

	return dlqMetrics.totalFailures, reasonCopy, oldestAge, dlqMetrics.lastFailureTime
}

// ResetMetrics resets DLQ metrics (useful for testing)
func ResetDLQMetrics() {
	dlqMetrics.mu.Lock()
	defer dlqMetrics.mu.Unlock()

	dlqMetrics.totalFailures = 0
	dlqMetrics.failuresByReason = make(map[string]int64)
	dlqMetrics.lastFailureTime = time.Time{}
}
