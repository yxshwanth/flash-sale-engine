package common

import (
	"os"

	"github.com/sirupsen/logrus"
)

var Logger *logrus.Logger

func InitLogger() *logrus.Logger {
	logger := logrus.New()
	
	// Configure JSON formatter for structured logging
	// JSON format enables easy parsing by log aggregation tools (ELK, Splunk, etc.)
	logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02T15:04:05.000Z07:00", // ISO 8601 format
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime:  "timestamp",
			logrus.FieldKeyLevel: "level",
			logrus.FieldKeyMsg:   "message",
		},
	})
	
	// Set log level from environment variable (LOG_LEVEL) or default to INFO
	// Allows runtime log level adjustment without code changes
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}
	
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		level = logrus.InfoLevel // Default to INFO if invalid level specified
	}
	logger.SetLevel(level)
	
	// Output to stdout for containerized environments
	// Logs are captured by Docker/Kubernetes logging infrastructure
	logger.SetOutput(os.Stdout)
	
	Logger = logger
	return logger
}

// WithCorrelationID creates a logger entry with correlation ID for request tracing
// All log entries created from this will include the correlation_id field
// This enables tracing a single request across gateway and processor services
func WithCorrelationID(correlationID string) *logrus.Entry {
	if Logger == nil {
		InitLogger()
	}
	return Logger.WithField("correlation_id", correlationID)
}

