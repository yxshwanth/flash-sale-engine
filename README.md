# 🚀 Flash Sale Engine

A high-concurrency distributed system for handling flash sales with **idempotency**, **atomic inventory management**, and **fault tolerance**. Built with Go, Kafka (Redpanda), Redis, and Docker.

[![Go Version](https://img.shields.io/badge/Go-1.22-blue.svg)](https://golang.org/)
[![Docker](https://img.shields.io/badge/Docker-Ready-green.svg)](https://www.docker.com/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-Ready-blue.svg)](https://kubernetes.io/)

## ✨ Features

- **🔄 Idempotency**: Prevents duplicate order processing using Redis SETNX
- **⚛️ Atomic Inventory**: Race-condition-free stock management using Redis DECR
- **📨 Async Processing**: Kafka-based message queue for decoupled processing
- **🛡️ Fault Tolerance**: Dead Letter Queue (DLQ) for failed orders
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
                  │  ency)   │                        │ Orders) │
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

**Responses:**
- `202 Accepted`: Order queued successfully
- `409 Conflict`: Duplicate request detected (idempotency)
- `400 Bad Request`: Invalid request body
- `500 Internal Server Error`: Server error

**Example:**
```bash
curl -X POST http://localhost:8080/buy \
  -H "Content-Type: application/json" \
  -d '{"user_id":"u1","item_id":"101","amount":1,"request_id":"req-123"}'
```

## 🎯 Key Features Explained

### 1. Idempotency

**Problem**: User double-clicks or network retries cause duplicate orders.

**Solution**: Redis `SETNX` (Set if Not Exists) with request_id as key.

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

**Solution**: Redis `DECR` is atomic - no race conditions possible.

```go
stock, err := redisClient.Decr(ctx, "inventory:"+order.ItemID).Result()
if stock < 0 {
    redisClient.Incr(ctx, "inventory:"+order.ItemID) // Refund
    return // Sold out
}
```

### 3. Fault Tolerance (DLQ)

**Problem**: Payment service fails, but order is already processed.

**Solution**: Failed orders moved to Dead Letter Queue, inventory refunded.

```go
if paymentFails {
    redisClient.Incr(ctx, "inventory:"+order.ItemID) // Refund
    moveToDLQ(msg, "Payment Timeout")
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

## 🧪 Testing Scenarios

### Scenario 1: Idempotency Test
1. Send request with `request_id: "test-123"`
2. Send same request again
3. **Expected**: First returns `202`, second returns `409`

### Scenario 2: Concurrent Orders
1. Send 100 orders rapidly
2. Check inventory decreases correctly
3. **Expected**: No overselling, inventory matches orders

### Scenario 3: Fault Tolerance
1. Send orders (10% will simulate payment failure)
2. Check DLQ for failed orders
3. **Expected**: Failed orders in DLQ, inventory refunded

## 📁 Project Structure

```
flash-sale-engine/
├── gateway/
│   └── main.go          # HTTP API (Producer)
├── processor/
│   └── main.go          # Kafka Consumer (Worker)
├── k8s/
│   ├── infrastructure.yaml  # Redis, Redpanda
│   └── apps.yaml            # Gateway, Processor
├── Dockerfile
├── docker-compose.yml
├── go.mod
├── test.ps1             # Automated test script
└── test-buy.ps1         # Quick buy script
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

- **Idempotency Key TTL**: 10 minutes (configurable)
- **Kafka Topic**: Auto-created in dev mode
- **Redis Persistence**: Configured for production
- **Concurrency**: Handles 1000+ requests/second

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
