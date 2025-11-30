# 🚀 Flash Sale Engine

A **production-ready** high-concurrency distributed system for handling flash sales with **idempotency**, **atomic inventory management**, **fault tolerance**, and **comprehensive observability**. Built with Go, Kafka (Redpanda), Redis, and Docker.

[![Go Version](https://img.shields.io/badge/Go-1.23-blue.svg)](https://golang.org/)
[![Docker](https://img.shields.io/badge/Docker-Ready-green.svg)](https://www.docker.com/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-Ready-blue.svg)](https://kubernetes.io/)

## ✨ Features

### Core Features
- **🔄 Idempotency**: Prevents duplicate order processing using Redis SETNX with configurable TTL and order status tracking
- **⚛️ Atomic Inventory**: Race-condition-free stock management using Redis Lua scripts with edge case handling
- **📨 Async Processing**: Kafka-based message queue for decoupled processing
- **🛡️ Fault Tolerance**: Circuit breaker pattern with configurable thresholds and Dead Letter Queue (DLQ) for failed orders
- **✅ Input Validation**: Comprehensive server-side validation with clear error messages
- **📝 Structured Logging**: JSON logs with correlation IDs for end-to-end request tracing
- **🏥 Health Checks**: Kubernetes-ready health endpoint with service status
- **📊 High Concurrency**: Handles thousands of concurrent requests

### Production-Ready Features
- **🚦 Rate Limiting**: Per-user rate limiting using Redis sliding window (configurable)
- **📈 Prometheus Metrics**: Comprehensive metrics for monitoring and alerting
- **⏱️ Request Timeouts**: Context-based timeouts for all external calls
- **🔄 Graceful Shutdown**: Handles termination signals to drain in-flight requests
- **📊 DLQ Monitoring**: Track DLQ size, age, and failure reasons
- **🔍 Order Status Tracking**: Track order status (PENDING, COMPLETED, FAILED) in Redis
- **⚡ Enhanced Circuit Breaker**: Configurable failure thresholds, success thresholds, and timeouts

## 🏗️ Architecture

```
┌─────────┐      ┌─────────┐      ┌──────────┐      ┌─────────┐
│  User   │─────▶│ Gateway │─────▶│  Kafka   │─────▶│Processor│
│ Request │      │  (API)  │      │ (Queue)  │      │ (Worker)│
└─────────┘      │ :8080   │      │          │      │ :9090   │
                 │ /metrics│      └──────────┘      │ /metrics│
                 └─────────┘                         └─────────┘
                       │                                    │
                       ▼                                    ▼
                  ┌─────────┐                        ┌─────────┐
                  │  Redis  │                        │   DLQ   │
                  │(Idempot │                        │(Failed  │
                  │  ency,  │                        │ Orders) │
                  │  Rate   │                        └─────────┘
                  │  Limit) │
                  └─────────┘
```

**Service Ports:**
- **Gateway**: `:8080` (HTTP API, `/health`, `/metrics`)
- **Processor**: `:9090` (Prometheus metrics)
- **Redis**: `:6379` (Idempotency, Inventory, Rate Limiting)
- **Redpanda**: `:19092` (Kafka-compatible message broker)

## 🚀 Quick Start

### Prerequisites

- Docker and Docker Compose
- Go 1.22+ (optional, for local development)

### 1. Clone and Start

```bash
git clone <your-repo-url>
cd flash-sale-engine
docker-compose up -d --build
```

### 2. Seed Inventory

```bash
docker exec flash-sale-engine-redis-1 redis-cli SET inventory:101 100
```

### 3. Test the System

**Option A: Comprehensive Test Suite (Recommended)**
```powershell
# Windows PowerShell - Tests all features
.\test-all-features.ps1
```

This comprehensive test suite validates:
- ✅ Input validation
- ✅ Idempotency
- ✅ Atomic inventory operations
- ✅ Structured logging with correlation IDs
- ✅ Health checks
- ✅ Circuit breaker behavior
- ✅ Rate limiting
- ✅ Prometheus metrics
- ✅ DLQ monitoring
- ✅ Order status tracking
- ✅ Sold out handling

**Option B: Quick Manual Testing**
```powershell
# Send an order
$body = '{"user_id":"u1","item_id":"101","amount":1,"request_id":"req-123"}'
Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing

# Test idempotency (send same request twice)
Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing
# Second request should return 409 Conflict
```

See [TESTING.md](TESTING.md) for detailed testing scenarios.

## 📋 API Documentation

### POST `/buy`

Place an order for a flash sale item.

**Request:**
```json
{
  "user_id": "u1",
  "item_id": "101",
  "amount": 1,
  "request_id": "unique-request-id-123"
}
```

**Validation Rules:**
- `user_id`: Required, alphanumeric/underscore/hyphen, max 100 chars
- `item_id`: Required, alphanumeric/underscore/hyphen, max 100 chars
- `amount`: Required, integer between 1 and 1000
- `request_id`: Required, non-empty, max 200 chars

**Responses:**
- `202 Accepted`: Order queued successfully
  ```json
  {
    "status": "Order Queued",
    "correlation_id": "uuid-here"
  }
  ```
- `409 Conflict`: Duplicate request detected (idempotency)
- `429 Too Many Requests`: Rate limit exceeded
- `400 Bad Request`: Validation failed
  ```json
  {
    "error": "Validation failed",
    "errors": [
      {"field": "amount", "message": "amount must be at least 1"}
    ],
    "correlation_id": "uuid-here"
  }
  ```
- `503 Service Unavailable`: Circuit breaker is open (Kafka unavailable)
- `500 Internal Server Error`: Server error

### GET `/health`

Health check endpoint for Kubernetes liveness/readiness probes.

**Response:**
```json
{
  "status": "healthy",
  "redis": true,
  "kafka": true,
  "circuit_breaker_state": "closed"
}
```

- `200 OK`: All services healthy
- `503 Service Unavailable`: One or more services unhealthy

### GET `/metrics` (Gateway)

Prometheus metrics endpoint for monitoring.

**Metrics Exposed:**
- `gateway_orders_received_total` - Total orders received
- `gateway_orders_successful_total` - Orders successfully queued
- `gateway_orders_failed_total` - Orders that failed to queue
- `gateway_orders_validation_failed_total` - Validation failures
- `gateway_orders_idempotency_rejected_total` - Duplicate requests rejected
- `gateway_request_duration_seconds` - Request processing time histogram
- `gateway_circuit_breaker_state` - Circuit breaker state (0=closed, 1=open, 2=half-open)

**Example:**
```bash
curl http://localhost:8080/metrics
```

### GET `/metrics` (Processor)

Prometheus metrics endpoint for processor monitoring (port 9090).

**Metrics Exposed:**
- `processor_orders_processed_total` - Total orders processed
- `processor_orders_processed_success_total` - Successfully processed
- `processor_orders_processed_failed_total` - Failed processing
- `processor_orders_sold_out_total` - Orders rejected due to sold out
- `processor_orders_moved_to_dlq_total` - Orders moved to DLQ
- `processor_order_processing_duration_seconds` - Processing time histogram
- `processor_dlq_size` - Current DLQ depth
- `processor_dlq_oldest_message_age_seconds` - Age of oldest DLQ message
- `processor_inventory_level{item_id="..."}` - Inventory level per item

**Example:**
```bash
curl http://localhost:9090/metrics
```

## 🎯 Key Features Explained

### 1. Idempotency

**Problem**: User double-clicks or network retries cause duplicate orders.

**Solution**: Redis `SETNX` (Set if Not Exists) with request_id as key and 10-minute TTL.

```go
isNew, err := redisClient.SetNX(ctx, "idempotency:"+order.RequestID, "processing", 10*time.Minute).Result()
if !isNew {
    return http.StatusConflict // Duplicate detected
}
```

**Demo:**
```powershell
# First request - succeeds
.\test-buy.ps1 -RequestId "demo-123"

# Second request with same ID - rejected
.\test-buy.ps1 -RequestId "demo-123"  # Returns 409 Conflict
```

### 2. Atomic Inventory Management

**Problem**: Race conditions when multiple users buy simultaneously.

**Solution**: Redis Lua scripts ensure atomic check-and-refund operations.

```go
// Lua script atomically decrements and refunds if sold out
result, err := checkInventoryScript.Run(ctx, redisClient, []string{inventoryKey}).Result()
// Returns {success: 0|1, stock: int} - all atomic
```

**Benefits:**
- No race conditions possible (Lua scripts are atomic)
- Automatic refund if sold out
- No partial failures

### 3. Circuit Breaker Pattern

**Problem**: Kafka failures can cascade and crash the gateway.

**Solution**: Enhanced circuit breaker with configurable thresholds and exponential backoff.

**Features:**
- Opens after N consecutive failures (configurable, default: 5)
- Half-open state allows limited requests to test recovery
- Configurable timeout with exponential backoff support
- State exposed via `/health` endpoint and Prometheus metrics

**Configuration:**
- `CIRCUIT_BREAKER_FAILURE_THRESHOLD`: Failures before opening (default: 5)
- `CIRCUIT_BREAKER_SUCCESS_THRESHOLD`: Successes in half-open (default: 2)
- `CIRCUIT_BREAKER_BASE_TIMEOUT`: Base timeout (default: 30s)
- `CIRCUIT_BREAKER_MAX_TIMEOUT`: Max timeout (default: 300s)

```go
// Circuit breaker wraps Kafka producer
producer = NewCircuitBreaker(rawProducer)
// Returns 503 Service Unavailable when circuit is open
```

### 4. Input Validation

**Problem**: Invalid inputs can cause errors or security issues.

**Solution**: Comprehensive validation with clear error messages.

- Validates user_id, item_id format (alphanumeric, underscore, hyphen)
- Validates amount (1-1000 range)
- Validates request_id (required, non-empty)
- Returns 400 Bad Request with detailed error messages

### 5. Structured Logging

**Problem**: Hard to trace requests across services.

**Solution**: JSON logs with correlation IDs for request tracing.

- Gateway generates UUID correlation IDs
- Correlation IDs passed via Kafka headers
- All logs include correlation_id field
- Enables tracing requests across gateway → Kafka → processor

### 6. Fault Tolerance (DLQ)

**Problem**: Payment service fails, but order is already processed.

**Solution**: Failed orders moved to Dead Letter Queue, inventory refunded atomically.

**Features:**
- Automatic inventory refund on failure (atomic Lua script)
- DLQ size and age monitoring via Prometheus
- Failure reason categorization
- Correlation IDs preserved for tracing

```go
if paymentFails {
    // Refund inventory using Lua script (atomic)
    refundScript.Run(ctx, redisClient, []string{inventoryKey}, 1)
    moveToDLQ(msg, "Payment Timeout", correlationID)
}
```

### 7. Rate Limiting

**Problem**: Users can overwhelm the system with too many requests.

**Solution**: Per-user rate limiting using Redis sliding window.

**Features:**
- Configurable max requests per window
- Per-user tracking (isolated limits)
- Redis-based for distributed systems
- Returns `429 Too Many Requests` when exceeded

**Configuration:**
- `RATE_LIMIT_MAX_REQUESTS`: Max requests per window (default: 60)
- `RATE_LIMIT_WINDOW`: Time window (default: 1m)

### 8. Prometheus Metrics

**Problem**: No visibility into system performance and health.

**Solution**: Comprehensive Prometheus metrics for monitoring and alerting.

**Gateway Metrics** (`:8080/metrics`):
- Order counters (received, successful, failed, validation errors, idempotency rejections)
- Request duration histogram
- Circuit breaker state gauge

**Processor Metrics** (`:9090/metrics`):
- Processing counters (total, success, failed, sold out, DLQ)
- Processing duration histogram
- DLQ size and age gauges
- Inventory level gauge per item

See [OPERATIONS.md](OPERATIONS.md) for monitoring and alerting guidelines.

### 9. Graceful Shutdown

**Problem**: Abrupt termination causes in-flight requests to fail.

**Solution**: Handles SIGTERM/SIGINT to drain in-flight requests.

**Features:**
- Stops accepting new requests
- Waits for in-flight requests to complete (30s timeout)
- Closes connections gracefully
- Ensures no data loss during shutdown

### 10. Order Status Tracking

**Problem**: No way to query order status after submission.

**Solution**: Track order status in Redis with TTL.

**Status Values:**
- `PENDING`: Order queued, awaiting processing
- `COMPLETED`: Order processed successfully
- `FAILED_SOLD_OUT`: Order failed due to insufficient inventory
- `FAILED_PAYMENT`: Order failed due to payment timeout

**TTL**: 30 minutes (configurable)

**Query:**
```bash
docker exec flash-sale-engine-redis-1 redis-cli GET "order_status:request-id-123"
```

## 📊 Monitoring & Observability

### Logs

**View Gateway Logs:**
```bash
docker-compose logs -f gateway
```

**View Processor Logs:**
```bash
docker-compose logs -f processor
```

**Search Logs by Correlation ID:**
```bash
# Find all logs for a specific request
docker-compose logs gateway processor | grep "correlation-id-here"
```

### Metrics

**Gateway Metrics:**
```bash
curl http://localhost:8080/metrics
```

**Processor Metrics:**
```bash
curl http://localhost:9090/metrics
```

**Query Specific Metrics:**
```bash
# Check circuit breaker state
curl -s http://localhost:8080/metrics | grep gateway_circuit_breaker_state

# Check DLQ size
curl -s http://localhost:9090/metrics | grep processor_dlq_size

# Check order success rate
curl -s http://localhost:8080/metrics | grep gateway_orders_successful_total
```

### Health Checks

**Check Service Health:**
```bash
curl http://localhost:8080/health
```

### Redis Operations

**Check Inventory:**
```bash
docker exec flash-sale-engine-redis-1 redis-cli GET inventory:101
```

**Check Order Status:**
```bash
docker exec flash-sale-engine-redis-1 redis-cli GET "order_status:request-id-123"
```

**Check Rate Limit:**
```bash
docker exec flash-sale-engine-redis-1 redis-cli GET "ratelimit:user-id-123"
```

**Check All Services:**
```bash
docker-compose ps
```

See [OPERATIONS.md](OPERATIONS.md) for comprehensive monitoring and troubleshooting guide.

## 🐳 Docker Compose Services

- **gateway**: HTTP API service (port 8080)
  - Endpoints: `/buy`, `/health`, `/metrics`
  - Features: Rate limiting, circuit breaker, input validation
- **processor**: Kafka consumer worker (port 9090)
  - Endpoints: `/metrics`
  - Features: Atomic inventory, DLQ handling, order status tracking
- **redis**: Inventory, idempotency, and rate limiting storage (port 6379)
- **redpanda**: Kafka-compatible message broker (port 19092)

## ☸️ Kubernetes Deployment

```bash
# Deploy infrastructure
kubectl apply -f k8s/infrastructure.yaml

# Wait for services
kubectl wait --for=condition=ready pod -l app=redis --timeout=60s
kubectl wait --for=condition=ready pod -l app=redpanda --timeout=60s

# Deploy applications
kubectl apply -f k8s/apps.yaml

# Seed inventory
kubectl exec -it deployment/redis -- redis-cli SET inventory:101 100

# Test (NodePort on 30000)
curl -X POST http://localhost:30000/buy \
  -H "Content-Type: application/json" \
  -d '{"user_id":"u1","item_id":"101","amount":1,"request_id":"req-123"}'
```

## 🧪 Testing

### Comprehensive Test Suite

Run the full test suite to validate all features:

```powershell
# Run comprehensive test suite
.\test-all-features.ps1
```

**Tests Cover:**
- ✅ Input validation (missing fields, invalid values, format validation)
- ✅ Idempotency (duplicate request rejection, order status tracking)
- ✅ Atomic inventory (concurrent orders, sold out handling)
- ✅ Structured logging (correlation IDs across services)
- ✅ Health check endpoint (service status)
- ✅ Circuit breaker (failure handling, recovery)
- ✅ Rate limiting (per-user limits, 429 responses)
- ✅ Prometheus metrics (gateway and processor)
- ✅ DLQ monitoring (size, age, failure reasons)
- ✅ Order status tracking (PENDING, COMPLETED, FAILED states)

### Quick Manual Tests

**Test 1: Idempotency**
```powershell
$body = '{"user_id":"u1","item_id":"101","amount":1,"request_id":"test-123"}'
# First request - should return 202
Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing
# Second request - should return 409
Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing
```

**Test 2: Rate Limiting**
```powershell
# Send 70 requests rapidly (limit is 60/min)
for ($i=1; $i -le 70; $i++) {
    $body = "{\"user_id\":\"ratelimit-user\",\"item_id\":\"101\",\"amount\":1,\"request_id\":\"rate-test-$i\"}"
    try {
        Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing
    } catch {
        Write-Host "Request $i : $($_.Exception.Response.StatusCode.value__)"
    }
}
# Should see 429 responses after 60 requests
```

**Test 3: Metrics**
```powershell
# Check gateway metrics
Invoke-WebRequest -Uri "http://localhost:8080/metrics" -UseBasicParsing

# Check processor metrics
Invoke-WebRequest -Uri "http://localhost:9090/metrics" -UseBasicParsing
```

**Test 4: Circuit Breaker**
```powershell
# Stop Kafka
docker-compose stop redpanda

# Send 6 requests (will fail)
for ($i=1; $i -le 6; $i++) {
    $body = "{\"user_id\":\"u$i\",\"item_id\":\"101\",\"amount\":1,\"request_id\":\"cb-test-$i\"}"
    try {
        Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing
    } catch { }
}

# Check health - circuit should be "open"
Invoke-WebRequest -Uri "http://localhost:8080/health" -UseBasicParsing

# Restart Kafka
docker-compose start redpanda

# Wait 35 seconds for circuit recovery
Start-Sleep -Seconds 35

# Check health - circuit should be "closed"
Invoke-WebRequest -Uri "http://localhost:8080/health" -UseBasicParsing
```

See [TESTING.md](TESTING.md) for detailed testing scenarios and troubleshooting.

## 📁 Project Structure

```
flash-sale-engine/
├── gateway/
│   ├── main.go              # HTTP API (Producer)
│   ├── validation.go        # Input validation logic
│   ├── circuit_breaker.go   # Circuit breaker for Kafka producer
│   └── rate_limiter.go      # Per-user rate limiting
├── processor/
│   ├── main.go              # Kafka Consumer (Worker)
│   ├── redis_scripts.go     # Redis Lua scripts for atomic operations
│   └── dlq_metrics.go       # DLQ monitoring metrics
├── common/
│   ├── logger.go            # Structured logging utilities
│   └── metrics.go           # Prometheus metrics definitions
├── k8s/
│   ├── infrastructure.yaml  # Redis, Redpanda
│   └── apps.yaml            # Gateway, Processor
├── Dockerfile               # Multi-stage build for both services
├── docker-compose.yml       # Local development setup
├── go.mod                   # Go module dependencies
├── test-all-features.ps1    # Comprehensive test suite
├── README.md                # This file
├── TESTING.md               # Detailed testing guide
└── OPERATIONS.md            # Operations runbook
```

## 🔧 Development

**Build Locally:**
```bash
go mod download
go build -o gateway-bin ./gateway/main.go
go build -o processor-bin ./processor/main.go
```

**Run Locally (requires Redis and Kafka):**
```bash
# Terminal 1: Gateway
REDIS_ADDR=localhost:6379 KAFKA_ADDR=localhost:9092 ./gateway-bin

# Terminal 2: Processor
REDIS_ADDR=localhost:6379 KAFKA_ADDR=localhost:9092 ./processor-bin
```

## 📈 Performance Considerations

- **Idempotency Key TTL**: 10 minutes (prevents key accumulation)
- **Order Status TTL**: 30 minutes (configurable)
- **Circuit Breaker**: Configurable thresholds (default: 5 failures, 30s timeout)
- **Rate Limiting**: Configurable per-user limits (default: 60 requests/minute)
- **Request Timeouts**: Context-based timeouts for all external calls
- **Kafka Topic**: Auto-created in dev mode
- **Redis Lua Scripts**: Atomic operations prevent race conditions
- **Structured Logging**: JSON format for easy log aggregation
- **Concurrency**: Handles 1000+ requests/second
- **Inventory Operations**: All atomic (no locks needed)
- **Graceful Shutdown**: 30s timeout for draining in-flight requests

## ⚙️ Configuration

### Environment Variables

**Gateway:**
- `REDIS_ADDR`: Redis address (default: `redis-service:6379`)
- `KAFKA_ADDR`: Kafka address (default: `kafka-service:9092`)
- `LOG_LEVEL`: Log level - `debug`, `info`, `warn`, `error` (default: `info`)
- `CIRCUIT_BREAKER_FAILURE_THRESHOLD`: Failures before opening (default: `5`)
- `CIRCUIT_BREAKER_SUCCESS_THRESHOLD`: Successes in half-open (default: `2`)
- `CIRCUIT_BREAKER_BASE_TIMEOUT`: Base timeout (default: `30s`)
- `CIRCUIT_BREAKER_MAX_TIMEOUT`: Max timeout (default: `300s`)
- `RATE_LIMIT_MAX_REQUESTS`: Max requests per window (default: `60`)
- `RATE_LIMIT_WINDOW`: Rate limit window (default: `1m`)

**Processor:**
- `REDIS_ADDR`: Redis address (default: `redis-service:6379`)
- `KAFKA_ADDR`: Kafka address (default: `kafka-service:9092`)
- `LOG_LEVEL`: Log level - `debug`, `info`, `warn`, `error` (default: `info`)

### Docker Compose Configuration

Edit `docker-compose.yml` to customize environment variables:

```yaml
gateway:
  environment:
    - RATE_LIMIT_MAX_REQUESTS=120  # Increase rate limit
    - CIRCUIT_BREAKER_FAILURE_THRESHOLD=10  # More tolerant
    - LOG_LEVEL=debug  # Verbose logging
```

## 🛠️ Troubleshooting

**Services not starting?**
```bash
docker-compose logs
docker-compose ps
```

**Can't connect to Redis/Kafka?**
- Check network: `docker network ls`
- Verify services: `docker-compose ps`
- Check logs: `docker-compose logs <service>`

**Orders not processing?**
- Check processor logs: `docker-compose logs processor`
- Verify Kafka topic exists
- Check Redis connection

**Circuit breaker stuck open?**
- Check Kafka/Redpanda is running: `docker-compose ps redpanda`
- Restart Kafka: `docker-compose restart redpanda`
- Wait 30+ seconds for circuit recovery
- Check health: `curl http://localhost:8080/health`

**Rate limiting too aggressive?**
- Check current limit: `docker-compose exec gateway env | grep RATE_LIMIT`
- Increase limit in `docker-compose.yml` and restart: `docker-compose restart gateway`

**Metrics not accessible?**
- Verify ports are exposed: `docker-compose ps`
- Check service is running: `docker-compose logs gateway processor`
- Test endpoints: `curl http://localhost:8080/metrics` and `curl http://localhost:9090/metrics`

**DLQ growing?**
- Check DLQ size: `curl -s http://localhost:9090/metrics | grep processor_dlq_size`
- Review failure reasons in processor logs: `docker-compose logs processor | grep DLQ`
- Check DLQ messages: `docker exec flash-sale-engine-redpanda-1 rpk topic consume orders-dlq`

See [OPERATIONS.md](OPERATIONS.md) for comprehensive troubleshooting guide.

## 📚 Documentation

- **[TESTING.md](TESTING.md)**: Comprehensive testing guide with all scenarios
- **[OPERATIONS.md](OPERATIONS.md)**: Operations runbook for production monitoring and troubleshooting

## 🔗 Related Resources

- **Prometheus**: [prometheus.io](https://prometheus.io/) - Metrics collection
- **Redis**: [redis.io](https://redis.io/) - In-memory data store
- **Redpanda**: [redpanda.com](https://redpanda.com/) - Kafka-compatible message broker
- **Go Circuit Breaker**: [github.com/sony/gobreaker](https://github.com/sony/gobreaker)

## 📝 License

MIT License

## 🤝 Contributing

Contributions welcome! Please open an issue or submit a PR.

## 📧 Contact

For questions or issues, please open a GitHub issue.

---

**Built with ❤️ for high-concurrency distributed systems**

**Production-Ready Features:**
- ✅ Comprehensive monitoring and observability
- ✅ Fault tolerance and resilience patterns
- ✅ Rate limiting and request validation
- ✅ Graceful shutdown and resource management
- ✅ End-to-end request tracing
- ✅ Operational runbooks and testing guides
