# Comprehensive Testing Guide

This guide covers all features of the Flash Sale Engine and how to test them.

## Quick Start

```powershell
# Start services
docker-compose up -d

# Wait for services to be ready
Start-Sleep -Seconds 10

# Run comprehensive test suite
.\test-all-features.ps1
```

## Feature Test Matrix

### ✅ Feature 1: Input Validation

**What it does**: Validates all input fields before processing.

**How to test**:
```powershell
# Test missing user_id
curl -X POST http://localhost:8080/buy `
  -H "Content-Type: application/json" `
  -d '{"item_id":"101","amount":1,"request_id":"test"}'
# Expected: 400 Bad Request

# Test negative amount
curl -X POST http://localhost:8080/buy `
  -H "Content-Type: application/json" `
  -d '{"user_id":"u1","item_id":"101","amount":-1,"request_id":"test"}'
# Expected: 400 Bad Request

# Test amount too large
curl -X POST http://localhost:8080/buy `
  -H "Content-Type: application/json" `
  -d '{"user_id":"u1","item_id":"101","amount":2000,"request_id":"test"}'
# Expected: 400 Bad Request
```

**Expected Results**:
- Invalid inputs return `400 Bad Request`
- Error messages include field names and validation errors
- Valid requests proceed normally

---

### ✅ Feature 2: Idempotency

**What it does**: Prevents duplicate order processing using Redis SETNX.

**How to test**:
```powershell
# First request
$body = '{"user_id":"u1","item_id":"101","amount":1,"request_id":"unique-123"}'
curl -X POST http://localhost:8080/buy -H "Content-Type: application/json" -d $body
# Expected: 202 Accepted

# Duplicate request (same request_id)
curl -X POST http://localhost:8080/buy -H "Content-Type: application/json" -d $body
# Expected: 409 Conflict
```

**Expected Results**:
- First request: `202 Accepted`
- Duplicate request: `409 Conflict`
- Response includes correlation ID

---

### ✅ Feature 3: Atomic Inventory (Lua Scripts)

**What it does**: Uses Redis Lua scripts for atomic inventory operations.

**How to test**:
```powershell
# Seed inventory
docker exec flash-sale-engine-redis-1 redis-cli SET inventory:101 10

# Send 15 rapid orders
for ($i=1; $i -le 15; $i++) {
    $body = "{\"user_id\":\"u$i\",\"item_id\":\"101\",\"amount\":1,\"request_id\":\"atomic-$i\"}"
    Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing
}

# Check inventory (should never go below 0)
docker exec flash-sale-engine-redis-1 redis-cli GET inventory:101
```

**Expected Results**:
- Inventory never goes negative
- All operations are atomic (no race conditions)
- Sold out orders are automatically refunded by Lua script

**Key Code**: `processor/redis_scripts.go` - Lua script ensures DECR and refund are atomic

---

### ✅ Feature 4: Structured Logging with Correlation IDs

**What it does**: All logs include correlation IDs for request tracing.

**How to test**:
```powershell
# Send an order
$body = '{"user_id":"u1","item_id":"101","amount":1,"request_id":"log-test"}'
$response = Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing
$correlationId = ($response.Content | ConvertFrom-Json).correlation_id

# Check gateway logs
docker logs flash-sale-engine-gateway-1 --tail 20 | Select-String $correlationId

# Check processor logs
docker logs flash-sale-engine-processor-1 --tail 20 | Select-String $correlationId
```

**Expected Results**:
- All logs are JSON formatted
- Correlation ID appears in gateway and processor logs
- Can trace a request across services

**Key Code**: 
- `common/logger.go` - Structured JSON logger
- `gateway/main.go` - Generates UUID correlation IDs
- `processor/main.go` - Extracts correlation IDs from Kafka headers

---

### ✅ Feature 5: Health Check Endpoint

**What it does**: Provides health status of Redis, Kafka, and circuit breaker.

**How to test**:
```powershell
# Check health
curl http://localhost:8080/health

# Expected JSON response:
# {
#   "status": "healthy",
#   "redis": true,
#   "kafka": true,
#   "circuit_breaker_state": "Closed"
# }
```

**Expected Results**:
- Returns `200 OK` when healthy
- Returns `503 Service Unavailable` when unhealthy
- Shows circuit breaker state

**Key Code**: `gateway/main.go` - `/health` endpoint

---

### ✅ Feature 6: Circuit Breaker

**What it does**: Prevents cascading failures when Kafka is down.

**How to test**:
```powershell
# Stop Kafka/Redpanda
docker-compose stop redpanda

# Send multiple requests (will fail)
for ($i=1; $i -le 6; $i++) {
    $body = "{\"user_id\":\"u$i\",\"item_id\":\"101\",\"amount\":1,\"request_id\":\"cb-test-$i\"}"
    try {
        Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing
    } catch {
        Write-Host "Request $i : $($_.Exception.Response.StatusCode.value__)"
    }
}

# Check health endpoint (circuit should be Open)
curl http://localhost:8080/health

# Restart Kafka
docker-compose start redpanda

# Wait for circuit to recover (30 seconds timeout)
Start-Sleep -Seconds 35

# Check health again (circuit should be Closed)
curl http://localhost:8080/health
```

**Expected Results**:
- After 5 failures, circuit opens
- Gateway returns `503 Service Unavailable` when circuit is open
- Circuit recovers after timeout period

**Key Code**: `gateway/circuit_breaker.go` - Circuit breaker implementation

---

### ✅ Feature 7: Dead Letter Queue (DLQ)

**What it does**: Failed orders are moved to DLQ for manual processing.

**How to test**:
```powershell
# Send orders (10% will fail due to simulated payment timeout)
for ($i=1; $i -le 20; $i++) {
    $body = "{\"user_id\":\"u$i\",\"item_id\":\"101\",\"amount\":1,\"request_id\":\"dlq-test-$i\"}"
    Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing
}

# Check processor logs for DLQ messages
docker logs flash-sale-engine-processor-1 --tail 50 | Select-String "DLQ"
```

**Expected Results**:
- ~10% of orders fail (simulated payment timeout)
- Failed orders moved to `orders-dlq` topic
- Inventory is refunded for failed orders
- Correlation IDs preserved in DLQ messages

**Key Code**: `processor/main.go` - `moveToDLQ()` function

---

## Manual Testing Scenarios

### Scenario 1: High Concurrency Test

```powershell
# Seed inventory
docker exec flash-sale-engine-redis-1 redis-cli SET inventory:101 100

# Send 100 concurrent requests
$jobs = 1..100 | ForEach-Object {
    $body = "{\"user_id\":\"u$_\",\"item_id\":\"101\",\"amount\":1,\"request_id\":\"concurrent-$_\"}"
    Start-Job -ScriptBlock {
        param($uri, $body)
        try {
            Invoke-WebRequest -Uri $uri -Method POST -Body $body -ContentType "application/json" -UseBasicParsing
            return "SUCCESS"
        } catch {
            return "FAILED"
        }
    } -ArgumentList "http://localhost:8080/buy", $body
}

# Wait and check results
$results = $jobs | Wait-Job | Receive-Job
$jobs | Remove-Job
$successCount = ($results | Where-Object { $_ -eq "SUCCESS" }).Count
Write-Host "Success: $successCount / 100"

# Verify inventory
docker exec flash-sale-engine-redis-1 redis-cli GET inventory:101
```

### Scenario 2: Correlation ID Tracing

```powershell
# Send order and capture correlation ID
$body = '{"user_id":"u1","item_id":"101","amount":1,"request_id":"trace-test"}'
$response = Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing
$correlationId = ($response.Content | ConvertFrom-Json).correlation_id

# Trace through all logs
Write-Host "Gateway logs:"
docker logs flash-sale-engine-gateway-1 --tail 50 | Select-String $correlationId

Write-Host "`nProcessor logs:"
docker logs flash-sale-engine-processor-1 --tail 50 | Select-String $correlationId
```

### Scenario 3: Circuit Breaker Recovery

```powershell
# Monitor circuit breaker state
while ($true) {
    $health = (Invoke-WebRequest -Uri "http://localhost:8080/health" -UseBasicParsing).Content | ConvertFrom-Json
    Write-Host "$(Get-Date -Format 'HH:mm:ss') - Circuit: $($health.circuit_breaker_state)"
    Start-Sleep -Seconds 5
}
```

## Verification Checklist

- [ ] Input validation rejects invalid requests
- [ ] Idempotency prevents duplicate orders
- [ ] Inventory never goes negative (atomic operations)
- [ ] Correlation IDs appear in all logs
- [ ] Health endpoint shows service status
- [ ] Circuit breaker opens after failures
- [ ] DLQ receives failed orders
- [ ] Inventory refunded on failures
- [ ] High concurrency handled correctly
- [ ] No data corruption under load

## Troubleshooting

**Services not starting?**
```powershell
docker-compose logs
docker-compose ps
```

**Can't see correlation IDs in logs?**
```powershell
# Check if JSON logging is enabled
docker logs flash-sale-engine-gateway-1 --tail 5
# Should see JSON formatted logs
```

**Circuit breaker not opening?**
```powershell
# Check Kafka is actually down
docker-compose ps redpanda
# Send 6 requests to trigger circuit open
```

**Inventory mismatch?**
```powershell
# Reset inventory
docker exec flash-sale-engine-redis-1 redis-cli SET inventory:101 100
# Check all keys
docker exec flash-sale-engine-redis-1 redis-cli KEYS "*"
```

