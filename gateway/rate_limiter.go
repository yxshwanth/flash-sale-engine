package main

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter implements per-user rate limiting using Redis sliding window
type RateLimiter struct {
	redisClient *redis.Client
	maxRequests int
	windowSize  time.Duration
}

// NewRateLimiter creates a new rate limiter
// maxRequests: maximum requests allowed per window
// windowSize: time window (e.g., 1 minute)
func NewRateLimiter(redisClient *redis.Client, maxRequests int, windowSize time.Duration) *RateLimiter {
	return &RateLimiter{
		redisClient: redisClient,
		maxRequests: maxRequests,
		windowSize:  windowSize,
	}
}

// Allow checks if a request from userID should be allowed
// Returns true if request is allowed, false if rate limit exceeded
// Uses Redis sliding window algorithm with INCR and EXPIRE
func (rl *RateLimiter) Allow(ctx context.Context, userID string) (bool, error) {
	key := "ratelimit:" + userID
	
	// Increment counter for this user
	count, err := rl.redisClient.Incr(ctx, key).Result()
	if err != nil {
		// If Redis fails, allow request (fail open)
		// In production, you might want to fail closed or use local cache
		return true, err
	}
	
	// Set expiration on first request (sliding window)
	if count == 1 {
		rl.redisClient.Expire(ctx, key, rl.windowSize)
	}
	
	// Check if limit exceeded
	if count > int64(rl.maxRequests) {
		return false, nil
	}
	
	return true, nil
}

// GetRemainingRequests returns how many requests the user has remaining in current window
func (rl *RateLimiter) GetRemainingRequests(ctx context.Context, userID string) (int, error) {
	key := "ratelimit:" + userID
	count, err := rl.redisClient.Get(ctx, key).Int()
	if err == redis.Nil {
		// Key doesn't exist, user has full quota
		return rl.maxRequests, nil
	}
	if err != nil {
		return 0, err
	}
	
	remaining := rl.maxRequests - count
	if remaining < 0 {
		return 0, nil
	}
	return remaining, nil
}

