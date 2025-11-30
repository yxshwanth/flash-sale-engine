# Operations Runbook

## Overview
This document provides operational procedures for monitoring, troubleshooting, and maintaining the Flash Sale Engine in production.

## Monitoring

### Prometheus Metrics

Both services expose Prometheus metrics on `/metrics` endpoint:

**Gateway Metrics** (`:8080/metrics`):
- `gateway_orders_received_total` - Total orders received
- `gateway_orders_successful_total` - Orders successfully queued
- `gateway_orders_failed_total` - Orders that failed to queue
- `gateway_orders_validation_failed_total` - Validation failures
- `gateway_orders_idempotency_rejected_total` - Duplicate requests rejected
- `gateway_request_duration_seconds` - Request processing time histogram
- `gateway_circuit_breaker_state` - Circuit breaker state (0=closed, 1=open, 2=half-open)

**Processor Metrics** (`:9090/metrics`):
- `processor_orders_processed_total` - Total orders processed
- `processor_orders_processed_success_total` - Successfully processed
- `processor_orders_processed_failed_total` - Failed processing
- `processor_orders_sold_out_total` - Orders rejected due to sold out
- `processor_orders_moved_to_dlq_total` - Orders moved to DLQ
- `processor_order_processing_duration_seconds` - Processing time histogram
- `processor_dlq_size` - Current DLQ depth
- `processor_dlq_oldest_message_age_seconds` - Age of oldest DLQ message
- `processor_inventory_level{item_id="..."}` - Inventory level per item

### Health Checks

**Gateway Health** (`GET /health`):
```bash
curl http://localhost:8080/health
```

Response:
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

### Logging

All services use structured JSON logging with correlation IDs:

```json
{
  "timestamp": "2025-11-29T21:56:00.000Z",
  "level": "INFO",
  "message": "Order queued successfully",
  "correlation_id": "uuid-123",
  "service": "gateway",
  "event": "order_queued",
  "user_id": "u1",
  "item_id": "101",
  "processing_time_ms": 145
}
```

**Key Fields**:
- `correlation_id`: Trace requests across services
- `service`: Service name (gateway/processor)
- `event`: Event type (order_received, order_queued, order_processed, etc.)
- `processing_time_ms`: Request processing time

## Alerting Thresholds

### Critical Alerts

1. **Circuit Breaker Open**
   - Metric: `gateway_circuit_breaker_state == 1`
   - Action: Check Kafka connectivity, restart if needed
   - Impact: All orders rejected with 503

2. **DLQ Size Exceeds Threshold**
   - Metric: `processor_dlq_size > 100`
   - Action: Investigate failure reasons, process DLQ manually
   - Impact: Orders not being processed

3. **DLQ Age Too High**
   - Metric: `processor_dlq_oldest_message_age_seconds > 3600`
   - Action: Process oldest messages first
   - Impact: Stale orders in DLQ

4. **High Failure Rate**
   - Metric: `gateway_orders_failed_total / gateway_orders_received_total > 0.1`
   - Action: Check service health, review logs
   - Impact: 10%+ of orders failing

5. **Processing Time High**
   - Metric: `processor_order_processing_duration_seconds{p99} > 5`
   - Action: Check Redis/Kafka latency, scale processor
   - Impact: Slow order processing

### Warning Alerts

1. **Rate Limit Approaching**
   - Monitor: Rate limit rejections increasing
   - Action: Review rate limit configuration

2. **Inventory Low**
   - Metric: `processor_inventory_level < 10`
   - Action: Restock or prepare for sold out

## Troubleshooting

### Issue: Circuit Breaker Open

**Symptoms**:
- All requests return 503 Service Unavailable
- Health check shows `circuit_breaker_state: "open"`

**Diagnosis**:
```bash
# Check Kafka connectivity
docker exec flash-sale-engine-redpanda-1 rpk cluster info

# Check gateway logs
docker-compose logs gateway | grep -i "circuit"
```

**Resolution**:
1. Check if Kafka/Redpanda is running: `docker-compose ps redpanda`
2. Restart Kafka if needed: `docker-compose restart redpanda`
3. Wait 30 seconds for circuit breaker to attempt recovery
4. Check health endpoint: `curl http://localhost:8080/health`

### Issue: Orders Not Processing

**Symptoms**:
- Orders accepted but not processed
- Inventory not decreasing

**Diagnosis**:
```bash
# Check processor logs
docker-compose logs processor

# Check Kafka topic
docker exec flash-sale-engine-redpanda-1 rpk topic consume orders

# Check processor metrics
curl http://localhost:9090/metrics | grep processor_orders_processed
```

**Resolution**:
1. Check processor is running: `docker-compose ps processor`
2. Check Kafka connectivity from processor
3. Verify Redis connection
4. Restart processor if needed: `docker-compose restart processor`

### Issue: High DLQ Size

**Symptoms**:
- `processor_dlq_size` metric increasing
- Many failed orders

**Diagnosis**:
```bash
# Check DLQ messages
docker exec flash-sale-engine-redpanda-1 rpk topic consume orders-dlq

# Check failure reasons in logs
docker-compose logs processor | grep -i "dlq"
```

**Resolution**:
1. Identify failure pattern (check DLQ message headers for error reasons)
2. Common reasons:
   - `Payment Timeout`: Expected (10% simulation), can be ignored
   - `Redis Failure`: Check Redis health
   - `Invalid Order Format`: Check gateway message format
3. Process DLQ manually or implement retry logic

### Issue: Inventory Mismatch

**Symptoms**:
- Inventory count doesn't match expected value
- Negative inventory (shouldn't happen with Lua scripts)

**Diagnosis**:
```bash
# Check current inventory
docker exec flash-sale-engine-redis-1 redis-cli GET inventory:101

# Check order status keys
docker exec flash-sale-engine-redis-1 redis-cli KEYS "order_status:*"
```

**Resolution**:
1. Verify Lua scripts are being used (check processor logs)
2. Check for Redis connection issues during script execution
3. Manually correct inventory if needed:
   ```bash
   docker exec flash-sale-engine-redis-1 redis-cli SET inventory:101 100
   ```

### Issue: Rate Limiting Too Aggressive

**Symptoms**:
- Many 429 Too Many Requests responses
- Legitimate users being blocked

**Diagnosis**:
```bash
# Check rate limit configuration
docker-compose exec gateway env | grep RATE_LIMIT

# Check rate limit keys in Redis
docker exec flash-sale-engine-redis-1 redis-cli KEYS "ratelimit:*"
```

**Resolution**:
1. Adjust rate limit via environment variables:
   ```yaml
   # docker-compose.yml
   environment:
     RATE_LIMIT_MAX_REQUESTS: 120  # Increase from default 60
     RATE_LIMIT_WINDOW: 1m
   ```
2. Restart gateway: `docker-compose restart gateway`

## Configuration

### Environment Variables

**Gateway**:
- `REDIS_ADDR`: Redis address (default: `redis-service:6379`)
- `KAFKA_ADDR`: Kafka address (default: `kafka-service:9092`)
- `LOG_LEVEL`: Log level (default: `info`)
- `CIRCUIT_BREAKER_FAILURE_THRESHOLD`: Failures before opening (default: `5`)
- `CIRCUIT_BREAKER_SUCCESS_THRESHOLD`: Successes in half-open (default: `2`)
- `CIRCUIT_BREAKER_BASE_TIMEOUT`: Base timeout (default: `30s`)
- `CIRCUIT_BREAKER_MAX_TIMEOUT`: Max timeout (default: `300s`)
- `RATE_LIMIT_MAX_REQUESTS`: Max requests per window (default: `60`)
- `RATE_LIMIT_WINDOW`: Rate limit window (default: `1m`)

**Processor**:
- `REDIS_ADDR`: Redis address (default: `redis-service:6379`)
- `KAFKA_ADDR`: Kafka address (default: `kafka-service:9092`)
- `LOG_LEVEL`: Log level (default: `info`)

## Backup and Recovery

### Redis Backup

```bash
# Create backup
docker exec flash-sale-engine-redis-1 redis-cli SAVE
docker cp flash-sale-engine-redis-1:/data/dump.rdb ./backup-$(date +%Y%m%d).rdb

# Restore backup
docker cp ./backup-20251129.rdb flash-sale-engine-redis-1:/data/dump.rdb
docker-compose restart redis
```

### Kafka/Redpanda Backup

Redpanda data is stored in volumes. Backup the volume:
```bash
docker run --rm -v flash-sale-engine_redpanda-data:/data -v $(pwd):/backup alpine tar czf /backup/redpanda-backup-$(date +%Y%m%d).tar.gz /data
```

## Performance Tuning

### Scaling

**Horizontal Scaling**:
- Gateway: Stateless, can scale horizontally
- Processor: Use Kafka consumer groups for parallel processing

**Vertical Scaling**:
- Increase Redis memory for larger inventory
- Increase Kafka partitions for higher throughput

### Optimization

1. **Redis Connection Pooling**: Already configured in go-redis
2. **Kafka Batch Size**: Adjust producer batch size for throughput
3. **Lua Script Caching**: Redis caches Lua scripts automatically
4. **Circuit Breaker Tuning**: Adjust thresholds based on failure patterns

## Maintenance Windows

### Zero-Downtime Deployment

1. Deploy new version to new pods
2. Wait for health checks to pass
3. Gradually shift traffic
4. Monitor metrics for issues
5. Rollback if problems detected

### Graceful Shutdown

Services handle SIGTERM gracefully:
- Gateway: Stops accepting new requests, waits for in-flight (30s timeout)
- Processor: Stops consuming, processes current message (30s timeout)

## Emergency Procedures

### Complete System Failure

1. **Stop all services**: `docker-compose down`
2. **Check data integrity**: Verify Redis and Kafka data
3. **Restore from backup** if needed
4. **Restart services**: `docker-compose up -d`
5. **Verify health**: Check all health endpoints
6. **Monitor metrics**: Watch for anomalies

### Data Corruption

1. **Stop services**: Prevent further corruption
2. **Restore from backup**
3. **Verify inventory counts**
4. **Replay DLQ messages** if needed
5. **Restart services**

## Contact and Escalation

- **On-Call Engineer**: Check team rotation schedule
- **Critical Issues**: Escalate immediately
- **Documentation**: Update this runbook with new procedures

