package common

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// GatewayMetrics holds all Prometheus metrics for the gateway service
type GatewayMetrics struct {
	OrdersReceived      prometheus.Counter
	OrdersSuccessful    prometheus.Counter
	OrdersFailed        prometheus.Counter
	OrdersValidationFailed prometheus.Counter
	OrdersIdempotencyRejected prometheus.Counter
	RequestDuration     prometheus.Histogram
	CircuitBreakerState prometheus.Gauge
}

// ProcessorMetrics holds all Prometheus metrics for the processor service
type ProcessorMetrics struct {
	OrdersProcessed     prometheus.Counter
	OrdersProcessedSuccess prometheus.Counter
	OrdersProcessedFailed prometheus.Counter
	OrdersSoldOut       prometheus.Counter
	OrdersMovedToDLQ    prometheus.Counter
	ProcessingDuration prometheus.Histogram
	DLQSize            prometheus.Gauge
	DLQAge             prometheus.Gauge
	InventoryLevels    *prometheus.GaugeVec
}

var (
	GatewayMetricsInstance   *GatewayMetrics
	ProcessorMetricsInstance *ProcessorMetrics
)

// InitGatewayMetrics initializes Prometheus metrics for gateway
func InitGatewayMetrics() *GatewayMetrics {
	metrics := &GatewayMetrics{
		OrdersReceived: promauto.NewCounter(prometheus.CounterOpts{
			Name: "gateway_orders_received_total",
			Help: "Total number of orders received by gateway",
		}),
		OrdersSuccessful: promauto.NewCounter(prometheus.CounterOpts{
			Name: "gateway_orders_successful_total",
			Help: "Total number of orders successfully queued",
		}),
		OrdersFailed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "gateway_orders_failed_total",
			Help: "Total number of orders that failed to queue",
		}),
		OrdersValidationFailed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "gateway_orders_validation_failed_total",
			Help: "Total number of orders rejected due to validation errors",
		}),
		OrdersIdempotencyRejected: promauto.NewCounter(prometheus.CounterOpts{
			Name: "gateway_orders_idempotency_rejected_total",
			Help: "Total number of duplicate orders rejected",
		}),
		RequestDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "gateway_request_duration_seconds",
			Help:    "Request processing duration in seconds",
			Buckets: prometheus.DefBuckets,
		}),
		CircuitBreakerState: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "gateway_circuit_breaker_state",
			Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
		}),
	}
	GatewayMetricsInstance = metrics
	return metrics
}

// InitProcessorMetrics initializes Prometheus metrics for processor
func InitProcessorMetrics() *ProcessorMetrics {
	metrics := &ProcessorMetrics{
		OrdersProcessed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "processor_orders_processed_total",
			Help: "Total number of orders processed",
		}),
		OrdersProcessedSuccess: promauto.NewCounter(prometheus.CounterOpts{
			Name: "processor_orders_processed_success_total",
			Help: "Total number of orders processed successfully",
		}),
		OrdersProcessedFailed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "processor_orders_processed_failed_total",
			Help: "Total number of orders that failed processing",
		}),
		OrdersSoldOut: promauto.NewCounter(prometheus.CounterOpts{
			Name: "processor_orders_sold_out_total",
			Help: "Total number of orders rejected due to sold out inventory",
		}),
		OrdersMovedToDLQ: promauto.NewCounter(prometheus.CounterOpts{
			Name: "processor_orders_moved_to_dlq_total",
			Help: "Total number of orders moved to Dead Letter Queue",
		}),
		ProcessingDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "processor_order_processing_duration_seconds",
			Help:    "Order processing duration in seconds",
			Buckets: prometheus.DefBuckets,
		}),
		DLQSize: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "processor_dlq_size",
			Help: "Current number of messages in Dead Letter Queue",
		}),
		DLQAge: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "processor_dlq_oldest_message_age_seconds",
			Help: "Age of oldest message in DLQ in seconds",
		}),
		InventoryLevels: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "processor_inventory_level",
			Help: "Current inventory level for items",
		}, []string{"item_id"}),
	}
	ProcessorMetricsInstance = metrics
	return metrics
}

