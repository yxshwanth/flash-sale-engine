# 🚀 Flash Sale Engine

A high-concurrency distributed system for handling flash sales with **idempotency**, **atomic inventory management**, and **fault tolerance**. Built with Go, Kafka (Redpanda), Redis, and Docker.

[![Go Version](https://img.shields.io/badge/Go-1.22-blue.svg)](https://golang.org/)
[![Docker](https://img.shields.io/badge/Docker-Ready-green.svg)](https://www.docker.com/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-Ready-blue.svg)](https://kubernetes.io/)

## ✨ Features

- **🔄 Idempotency**: Prevents duplicate order processing using Redis SETNX with 10-minute TTL
- **⚛️ Atomic Inventory**: Race-condition-free stock management using Redis Lua scripts
- **📨 Async Processing**: Kafka-based message queue for decoupled processing
- **🛡️ Fault Tolerance**: Circuit breaker pattern and Dead Letter Queue (DLQ) for failed orders
- **✅ Input Validation**: Comprehensive validation with clear error messages
- **📝 Structured Logging**: JSON logs with correlation IDs for request tracing
- **🏥 Health Checks**: Kubernetes-ready health endpoint
- **📊 High Concurrency**: Handles thousands of concurrent requests

## 🏗️ Architecture

```
┌─────────┐      ┌─────────┐      ┌──────────┐      ┌─────────┐
│  User   │─────▶│ Gateway │─────▶│  Kafka   │─────▶│Processor│
│ Request │      │  (API)  │      │ (Queue)  │      │ (Worker)│
└─────────┘      └─────────┘      └──────────┘      └─────────┘
                       │                                    │
                       ▼                                    ▼
                  ┌─────────┐                        ┌─────────┐
                  │  Redis  │                        │   DLQ   │
                  │(Idempot │                        │(Failed  │
                  │  ency)  │                        │ Orders) │
                  └─────────┘                        └─────────┘
```

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

**Option A: Automated Test Script**
```powershell
# Windows PowerShell
.\test.ps1

# Linux/Mac
chmod +x test.sh
./test.sh
```

**Option B: Manual Testing**
```powershell
# Send an order
.\test-buy.ps1 -UserId "u1" -ItemId "101" -RequestId "req-123"

# Test idempotency (send same request twice)
.\test-buy.ps1 -UserId "u1" -ItemId "101" -RequestId "test-123"
.\test-buy.ps1 -UserId "u1" -ItemId "101" -RequestId "test-123"  # Should return 409
```

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

**Example:**
```bash
curl -X POST http://localhost:8080/buy \
  -H "Content-Type: application/json" \
  -d '{"user_id":"u1","item_id":"101","amount":1,"request_id":"req-123"}'
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

**Solution**: Circuit breaker opens after 5 consecutive failures, preventing cascading failures.

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

```go
if paymentFails {
    // Refund inventory using Lua script (atomic)
    refundScript.Run(ctx, redisClient, []string{inventoryKey}, 1)
    moveToDLQ(msg, "Payment Timeout", correlationID)
}
```

## 📊 Monitoring & Logs

**View Gateway Logs:**
```bash
docker-compose logs -f gateway
```

**View Processor Logs:**
```bash
docker-compose logs -f processor
```

**Check Inventory:**
```bash
docker exec flash-sale-engine-redis-1 redis-cli GET inventory:101
```

**Check All Services:**
```bash
docker-compose ps
```

## 🐳 Docker Compose Services

- **gateway**: HTTP API service (port 8080)
- **processor**: Kafka consumer worker
- **redis**: Inventory and idempotency storage (port 6379)
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

### Quick Test (All Features)
```powershell
# Run comprehensive test suite
.\test-all-features.ps1
```

Tests cover:
- ✅ Input validation (missing fields, invalid values)
- ✅ Idempotency (duplicate request rejection)
- ✅ Atomic inventory (concurrent orders)
- ✅ Structured logging (correlation IDs)
- ✅ Health check endpoint
- ✅ Sold out handling

### Manual Testing Scenarios

**Scenario 1: Idempotency Test**
1. Send request with `request_id: "test-123"`
2. Send same request again
3. **Expected**: First returns `202`, second returns `409`

**Scenario 2: Concurrent Orders**
1. Send 100 orders rapidly
2. Check inventory decreases correctly
3. **Expected**: No overselling, inventory matches orders

**Scenario 3: Fault Tolerance**
1. Send orders (10% will simulate payment failure)
2. Check DLQ for failed orders
3. **Expected**: Failed orders in DLQ, inventory refunded

**Scenario 4: Circuit Breaker**
1. Stop Kafka: `docker-compose stop redpanda`
2. Send 6 requests (will fail)
3. Check `/health` endpoint - circuit should be "Open"
4. Restart Kafka: `docker-compose start redpanda`
5. Wait 30 seconds, check `/health` - circuit should be "Closed"

See [TESTING.md](TESTING.md) for detailed testing guide.

## 📁 Project Structure

```
flash-sale-engine/
├── gateway/
│   ├── main.go              # HTTP API (Producer)
│   ├── validation.go        # Input validation logic
│   └── circuit_breaker.go   # Circuit breaker for Kafka producer
├── processor/
│   ├── main.go              # Kafka Consumer (Worker)
│   └── redis_scripts.go     # Redis Lua scripts for atomic operations
├── common/
│   └── logger.go            # Structured logging utilities
├── k8s/
│   ├── infrastructure.yaml  # Redis, Redpanda
│   └── apps.yaml            # Gateway, Processor
├── Dockerfile
├── docker-compose.yml
├── go.mod
├── test-all-features.ps1    # Comprehensive test suite
├── test-buy.ps1             # Quick buy script
└── TESTING.md               # Detailed testing guide
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
- **Circuit Breaker**: Opens after 5 consecutive failures, 30s timeout
- **Kafka Topic**: Auto-created in dev mode
- **Redis Lua Scripts**: Atomic operations prevent race conditions
- **Structured Logging**: JSON format for easy log aggregation
- **Concurrency**: Handles 1000+ requests/second
- **Inventory Operations**: All atomic (no locks needed)

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

## 📝 License

MIT License

## 🤝 Contributing

Contributions welcome! Please open an issue or submit a PR.

## 📧 Contact

For questions or issues, please open a GitHub issue.

---

**Built with ❤️ for high-concurrency distributed systems**
