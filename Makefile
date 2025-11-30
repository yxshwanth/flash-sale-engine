.PHONY: help build up down restart logs logs-gateway logs-processor test seed seed-item health metrics metrics-gateway metrics-processor inventory order-status clean rebuild ps test-order test-idempotency

# Default target
help:
	@echo "Flash Sale Engine - Makefile Commands"
	@echo ""
	@echo "Available commands:"
	@echo "  make build       - Build Docker images"
	@echo "  make up          - Start all services"
	@echo "  make down        - Stop all services"
	@echo "  make restart     - Restart all services"
	@echo "  make logs        - View logs from all services"
	@echo "  make logs-gateway - View gateway logs"
	@echo "  make logs-processor - View processor logs"
	@echo "  make test        - Run comprehensive test suite"
	@echo "  make seed        - Seed inventory (100 items for item_id '101')"
	@echo "  make seed-item   - Seed inventory for specific item (usage: make seed-item ITEM=102 QTY=50)"
	@echo "  make health      - Check service health"
	@echo "  make metrics     - View Prometheus metrics"
	@echo "  make metrics-gateway - View gateway metrics"
	@echo "  make metrics-processor - View processor metrics"
	@echo "  make inventory   - Check inventory for item (usage: make inventory ITEM=101)"
	@echo "  make order-status - Check order status (usage: make order-status REQ_ID=test-123)"
	@echo "  make clean       - Stop services and remove containers"
	@echo "  make rebuild     - Rebuild and restart services"
	@echo "  make ps          - Show running containers"
	@echo ""

# Build Docker images
build:
	@echo "Building Docker images..."
	docker-compose build

# Start all services
up:
	@echo "Starting services..."
	docker-compose up -d
	@echo "Waiting for services to be ready..."
	@sleep 5
	@echo "Services started. Use 'make health' to check status."

# Stop all services
down:
	@echo "Stopping services..."
	docker-compose down

# Restart all services
restart:
	@echo "Restarting services..."
	docker-compose restart

# View logs from all services
logs:
	docker-compose logs -f

# View gateway logs
logs-gateway:
	docker-compose logs -f gateway

# View processor logs
logs-processor:
	docker-compose logs -f processor

# Run comprehensive test suite (requires PowerShell on Windows, or bash script on Linux)
test:
	@echo "Running test suite..."
	@if command -v pwsh >/dev/null 2>&1; then \
		pwsh -File test-all-features.ps1; \
	elif [ -f test-all-features.sh ]; then \
		./test-all-features.sh; \
	else \
		echo "Error: test-all-features.ps1 requires PowerShell or test-all-features.sh"; \
		echo "For manual testing, use: make seed && curl -X POST http://localhost:8080/buy ..."; \
	fi

# Seed inventory (default: 100 items for item_id '101')
seed:
	@echo "Seeding inventory: 100 items for item_id '101'..."
	@docker exec flash-sale-engine-redis-1 redis-cli SET inventory:101 100 || \
		docker exec $$(docker ps -q -f name=redis) redis-cli SET inventory:101 100
	@echo "Inventory seeded successfully"

# Seed inventory for specific item
seed-item:
	@if [ -z "$(ITEM)" ] || [ -z "$(QTY)" ]; then \
		echo "Usage: make seed-item ITEM=102 QTY=50"; \
		exit 1; \
	fi
	@echo "Seeding inventory: $(QTY) items for item_id '$(ITEM)'..."
	@docker exec flash-sale-engine-redis-1 redis-cli SET inventory:$(ITEM) $(QTY) || \
		docker exec $$(docker ps -q -f name=redis) redis-cli SET inventory:$(ITEM) $(QTY)
	@echo "Inventory seeded successfully"

# Check service health
health:
	@echo "Checking service health..."
	@curl -s http://localhost:8080/health | python3 -m json.tool 2>/dev/null || \
		curl -s http://localhost:8080/health | python -m json.tool 2>/dev/null || \
		curl -s http://localhost:8080/health || \
		echo "Error: Could not check health. Is the gateway running?"

# View Prometheus metrics (both services)
metrics:
	@echo "=== Gateway Metrics (port 8080) ==="
	@curl -s http://localhost:8080/metrics | head -20
	@echo ""
	@echo "=== Processor Metrics (port 9090) ==="
	@curl -s http://localhost:9090/metrics | head -20

# View gateway metrics
metrics-gateway:
	@echo "Gateway Metrics:"
	@curl -s http://localhost:8080/metrics | grep -E "^gateway_" | head -20

# View processor metrics
metrics-processor:
	@echo "Processor Metrics:"
	@curl -s http://localhost:9090/metrics | grep -E "^processor_" | head -20

# Check inventory for specific item
inventory:
	@if [ -z "$(ITEM)" ]; then \
		echo "Usage: make inventory ITEM=101"; \
		exit 1; \
	fi
	@echo "Checking inventory for item_id '$(ITEM)'..."
	@docker exec flash-sale-engine-redis-1 redis-cli GET inventory:$(ITEM) || \
		docker exec $$(docker ps -q -f name=redis) redis-cli GET inventory:$(ITEM)

# Check order status
order-status:
	@if [ -z "$(REQ_ID)" ]; then \
		echo "Usage: make order-status REQ_ID=test-123"; \
		exit 1; \
	fi
	@echo "Checking order status for request_id '$(REQ_ID)'..."
	@docker exec flash-sale-engine-redis-1 redis-cli GET "order_status:$(REQ_ID)" || \
		docker exec $$(docker ps -q -f name=redis) redis-cli GET "order_status:$(REQ_ID)"

# Clean up: stop services and remove containers
clean:
	@echo "Cleaning up..."
	docker-compose down -v
	@echo "Cleanup complete"

# Rebuild and restart services
rebuild:
	@echo "Rebuilding and restarting services..."
	docker-compose up -d --build
	@echo "Waiting for services to be ready..."
	@sleep 5
	@echo "Services rebuilt and restarted. Use 'make health' to check status."

# Show running containers
ps:
	@docker-compose ps

# Quick test: send a test order
test-order:
	@echo "Sending test order..."
	@curl -X POST http://localhost:8080/buy \
		-H "Content-Type: application/json" \
		-d '{"user_id":"test-user","item_id":"101","amount":1,"request_id":"make-test-'"$$(date +%s)"'"}' \
		-w "\nHTTP Status: %{http_code}\n" \
		-s || echo "Error: Could not send order. Is the gateway running?"

# Test idempotency: send same request twice
test-idempotency:
	@echo "Testing idempotency..."
	@REQ_ID="idempotency-test-$$(date +%s)"; \
	echo "First request (should succeed):"; \
	curl -X POST http://localhost:8080/buy \
		-H "Content-Type: application/json" \
		-d "{\"user_id\":\"test-user\",\"item_id\":\"101\",\"amount\":1,\"request_id\":\"$$REQ_ID\"}" \
		-w "\nHTTP Status: %{http_code}\n" \
		-s; \
	echo ""; \
	echo "Second request (should return 409):"; \
	curl -X POST http://localhost:8080/buy \
		-H "Content-Type: application/json" \
		-d "{\"user_id\":\"test-user\",\"item_id\":\"101\",\"amount\":1,\"request_id\":\"$$REQ_ID\"}" \
		-w "\nHTTP Status: %{http_code}\n" \
		-s || echo "Error: Could not test idempotency. Is the gateway running?"

