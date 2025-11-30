package main

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	maxUserIDLength   = 100
	maxItemIDLength   = 100
	maxRequestIDLength = 200
	maxAmount          = 1000
	minAmount          = 1
)

var (
	// idPattern validates user_id and item_id format
	// Allows alphanumeric characters, underscores, and hyphens
	// Prevents injection attacks and ensures consistent ID format
	idPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

// ValidationError represents a validation error
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidateOrderRequest validates an order request
func ValidateOrderRequest(order *OrderRequest) []ValidationError {
	var errors []ValidationError

	// Validate UserID
	if order.UserID == "" {
		errors = append(errors, ValidationError{
			Field:   "user_id",
			Message: "user_id is required",
		})
	} else if len(order.UserID) > maxUserIDLength {
		errors = append(errors, ValidationError{
			Field:   "user_id",
			Message: fmt.Sprintf("user_id must be at most %d characters", maxUserIDLength),
		})
	} else if !idPattern.MatchString(order.UserID) {
		errors = append(errors, ValidationError{
			Field:   "user_id",
			Message: "user_id contains invalid characters (only alphanumeric, underscore, and hyphen allowed)",
		})
	}

	// Validate ItemID
	if order.ItemID == "" {
		errors = append(errors, ValidationError{
			Field:   "item_id",
			Message: "item_id is required",
		})
	} else if len(order.ItemID) > maxItemIDLength {
		errors = append(errors, ValidationError{
			Field:   "item_id",
			Message: fmt.Sprintf("item_id must be at most %d characters", maxItemIDLength),
		})
	} else if !idPattern.MatchString(order.ItemID) {
		errors = append(errors, ValidationError{
			Field:   "item_id",
			Message: "item_id contains invalid characters (only alphanumeric, underscore, and hyphen allowed)",
		})
	}

	// Validate Amount
	if order.Amount < minAmount {
		errors = append(errors, ValidationError{
			Field:   "amount",
			Message: fmt.Sprintf("amount must be at least %d", minAmount),
		})
	} else if order.Amount > maxAmount {
		errors = append(errors, ValidationError{
			Field:   "amount",
			Message: fmt.Sprintf("amount must be at most %d", maxAmount),
		})
	}

	// Validate RequestID
	if order.RequestID == "" {
		errors = append(errors, ValidationError{
			Field:   "request_id",
			Message: "request_id is required for idempotency",
		})
	} else if len(order.RequestID) > maxRequestIDLength {
		errors = append(errors, ValidationError{
			Field:   "request_id",
			Message: fmt.Sprintf("request_id must be at most %d characters", maxRequestIDLength),
		})
	} else {
		// RequestID format is more flexible (allows UUIDs, timestamps, etc.)
		// Only check that it's not empty or whitespace-only
		trimmed := strings.TrimSpace(order.RequestID)
		if trimmed == "" {
			errors = append(errors, ValidationError{
				Field:   "request_id",
				Message: "request_id cannot be empty or whitespace only",
			})
		}
	}

	return errors
}

