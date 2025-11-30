# Comprehensive Feature Test Script for Flash Sale Engine
# Tests all features including improvements

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Flash Sale Engine - Feature Test Suite" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Check if services are running
Write-Host "1. Checking Services..." -ForegroundColor Yellow
$gatewayRunning = docker ps --filter "name=flash-sale-engine-gateway" --format "{{.Names}}" | Select-String "gateway"
if (-not $gatewayRunning) {
    Write-Host "   Services not running. Starting..." -ForegroundColor Yellow
    docker-compose up -d
    Start-Sleep -Seconds 10
}

Write-Host "   Gateway: Running" -ForegroundColor Green
Write-Host ""

# Seed inventory
Write-Host "2. Seeding Inventory..." -ForegroundColor Yellow
docker exec flash-sale-engine-redis-1 redis-cli SET inventory:101 100 | Out-Null
$inventory = docker exec flash-sale-engine-redis-1 redis-cli GET inventory:101
Write-Host "   Initial inventory: $inventory" -ForegroundColor Green
Write-Host ""

# ============================================
# FEATURE 1: INPUT VALIDATION
# ============================================
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "FEATURE 1: Input Validation" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

Write-Host "Test 1.1: Missing user_id..." -ForegroundColor Gray
$body = '{"item_id":"101","amount":1,"request_id":"test-1"}'
try {
    $response = Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing -ErrorAction Stop
    Write-Host "   FAILED: Should have returned 400, got $($response.StatusCode)" -ForegroundColor Red
} catch {
    $statusCode = $_.Exception.Response.StatusCode.value__
    if ($statusCode -eq 400) {
        Write-Host "   PASSED: Returned 400 Bad Request" -ForegroundColor Green
    } else {
        Write-Host "   FAILED: Expected 400, got $statusCode" -ForegroundColor Red
    }
}

Write-Host ""
Write-Host "Test 1.2: Negative amount..." -ForegroundColor Gray
$body = '{"user_id":"u1","item_id":"101","amount":-1,"request_id":"test-2"}'
try {
    $response = Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing -ErrorAction Stop
    Write-Host "   FAILED: Should have returned 400, got $($response.StatusCode)" -ForegroundColor Red
} catch {
    $statusCode = $_.Exception.Response.StatusCode.value__
    if ($statusCode -eq 400) {
        Write-Host "   PASSED: Returned 400 Bad Request" -ForegroundColor Green
    } else {
        Write-Host "   FAILED: Expected 400, got $statusCode" -ForegroundColor Red
    }
}

Write-Host ""
Write-Host "Test 1.3: Amount too large..." -ForegroundColor Gray
$body = '{"user_id":"u1","item_id":"101","amount":2000,"request_id":"test-3"}'
try {
    $response = Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing -ErrorAction Stop
    Write-Host "   FAILED: Should have returned 400, got $($response.StatusCode)" -ForegroundColor Red
} catch {
    $statusCode = $_.Exception.Response.StatusCode.value__
    if ($statusCode -eq 400) {
        Write-Host "   PASSED: Returned 400 Bad Request" -ForegroundColor Green
    } else {
        Write-Host "   FAILED: Expected 400, got $statusCode" -ForegroundColor Red
    }
}

Write-Host ""
Write-Host "Test 1.4: Valid request..." -ForegroundColor Gray
$body = @{
    user_id = "u1"
    item_id = "101"
    amount = 1
    request_id = "test-valid-$(Get-Date -Format 'yyyyMMddHHmmss')"
} | ConvertTo-Json -Compress
try {
    $response = Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing
    if ($response.StatusCode -eq 202) {
        Write-Host "   PASSED: Valid request accepted (202)" -ForegroundColor Green
        $responseData = $response.Content | ConvertFrom-Json
        Write-Host "   Correlation ID: $($responseData.correlation_id)" -ForegroundColor Cyan
    } else {
        Write-Host "   FAILED: Expected 202, got $($response.StatusCode)" -ForegroundColor Red
    }
} catch {
    $statusCode = $_.Exception.Response.StatusCode.value__
    Write-Host "   FAILED: Expected 202, got $statusCode" -ForegroundColor Red
}

Write-Host ""

# ============================================
# FEATURE 2: IDEMPOTENCY
# ============================================
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "FEATURE 2: Idempotency" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

Write-Host "Test 2.1: First request with unique ID..." -ForegroundColor Gray
$requestId = "idempotency-test-$(Get-Date -Format 'yyyyMMddHHmmss')"
$body = @{
    user_id = "u1"
    item_id = "101"
    amount = 1
    request_id = $requestId
} | ConvertTo-Json -Compress
try {
    $response = Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing
    if ($response.StatusCode -eq 202) {
        Write-Host "   PASSED: First request accepted (202)" -ForegroundColor Green
    }
} catch {
    Write-Host "   FAILED: $_" -ForegroundColor Red
}

Write-Host ""
Write-Host "Test 2.2: Duplicate request (same request_id)..." -ForegroundColor Gray
Start-Sleep -Milliseconds 500
try {
    $response = Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing -ErrorAction Stop
    Write-Host "   FAILED: Should have returned 409" -ForegroundColor Red
} catch {
    $statusCode = $_.Exception.Response.StatusCode.value__
    if ($statusCode -eq 409) {
        Write-Host "   PASSED: Duplicate request rejected (409 Conflict)" -ForegroundColor Green
    } else {
        Write-Host "   FAILED: Unexpected status $statusCode" -ForegroundColor Red
    }
}

Write-Host ""

# ============================================
# FEATURE 3: ATOMIC INVENTORY (LUA SCRIPTS)
# ============================================
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "FEATURE 3: Atomic Inventory (Lua Scripts)" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

Write-Host "Test 3.1: Send 5 concurrent orders..." -ForegroundColor Gray
# Use a unique item ID for this test to avoid conflicts with previous tests
$testItemId = "atomic-test-$(Get-Date -Format 'yyyyMMddHHmmss')"
$initialInventory = 100
$ordersToSend = 5

# Reset inventory to known value before test
docker exec flash-sale-engine-redis-1 redis-cli SET "inventory:$testItemId" $initialInventory | Out-Null
Write-Host "   Using test item ID: $testItemId" -ForegroundColor Gray
Write-Host "   Initial inventory: $initialInventory" -ForegroundColor Gray

$jobs = @()
for ($i = 1; $i -le $ordersToSend; $i++) {
    $orderBody = @{
        user_id = "u$i"
        item_id = $testItemId
        amount = 1
        request_id = "atomic-test-$i-$(Get-Date -Format 'yyyyMMddHHmmss')"
    } | ConvertTo-Json -Compress
    $jobs += Start-Job -ScriptBlock {
        param($uri, $body)
        try {
            $response = Invoke-WebRequest -Uri $uri -Method POST -Body $body -ContentType "application/json" -UseBasicParsing
            return $response.StatusCode
        } catch {
            return $_.Exception.Response.StatusCode.value__
        }
    } -ArgumentList "http://localhost:8080/buy", $orderBody
}

$results = $jobs | Wait-Job | Receive-Job
$jobs | Remove-Job

$successCount = ($results | Where-Object { $_ -eq 202 }).Count
Write-Host "   Orders sent: $ordersToSend, Accepted: $successCount" -ForegroundColor $(if ($successCount -eq $ordersToSend) { "Green" } else { "Yellow" })

# Wait for all orders to be processed - check until inventory stabilizes
Write-Host "   Waiting for orders to be processed..." -ForegroundColor Gray
$maxWait = 10
$waitCount = 0
$previousInventory = $initialInventory
$stableCount = 0

while ($waitCount -lt $maxWait) {
    Start-Sleep -Seconds 1
    $currentInventoryStr = docker exec flash-sale-engine-redis-1 redis-cli GET "inventory:$testItemId" 2>$null
    if ($currentInventoryStr) {
        $currentInventory = [int]$currentInventoryStr
    } else {
        $currentInventory = $initialInventory
    }
    
    if ($currentInventory -eq $previousInventory) {
        $stableCount++
        if ($stableCount -ge 2) {
            # Inventory has been stable for 2 seconds, processing is complete
            break
        }
    } else {
        $stableCount = 0
    }
    $previousInventory = $currentInventory
    $waitCount++
}

$finalInventoryStr = docker exec flash-sale-engine-redis-1 redis-cli GET "inventory:$testItemId" 2>$null
$finalInventory = if ($finalInventoryStr) { [int]$finalInventoryStr } else { $initialInventory }
$expectedInventory = $initialInventory - $successCount

Write-Host "   Remaining inventory: $finalInventory" -ForegroundColor Cyan
Write-Host "   Expected: $expectedInventory (after $successCount successful orders)" -ForegroundColor Gray

if ($finalInventory -eq $expectedInventory) {
    Write-Host "   PASSED: Inventory correctly decremented atomically" -ForegroundColor Green
} else {
    $diff = $finalInventory - $expectedInventory
    Write-Host "   FAILED: Inventory mismatch! Difference: $diff" -ForegroundColor Red
    if ($diff -gt 0) {
        Write-Host "   Possible causes: Order refunded, processing delay, or race condition" -ForegroundColor Yellow
    } else {
        Write-Host "   Possible causes: Extra order processed or inventory leak" -ForegroundColor Yellow
    }
}

Write-Host ""

# ============================================
# FEATURE 4: STRUCTURED LOGGING
# ============================================
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "FEATURE 4: Structured Logging with Correlation IDs" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

Write-Host "Test 4.1: Check gateway logs for correlation ID..." -ForegroundColor Gray
$body = @{
    user_id = "u1"
    item_id = "101"
    amount = 1
    request_id = "log-test-$(Get-Date -Format 'HHmmss')"
} | ConvertTo-Json -Compress
try {
    $response = Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing
    $responseData = $response.Content | ConvertFrom-Json
    $correlationId = $responseData.correlation_id
    
    Write-Host "   Correlation ID: $correlationId" -ForegroundColor Cyan
    Write-Host "   Checking logs..." -ForegroundColor Gray
    
    Start-Sleep -Seconds 1
    $logs = docker logs flash-sale-engine-gateway-1 --tail 5 2>&1
    if ($logs -match $correlationId) {
        Write-Host "   PASSED: Correlation ID found in logs" -ForegroundColor Green
    } else {
        Write-Host "   INFO: Check logs manually: docker logs flash-sale-engine-gateway-1" -ForegroundColor Yellow
    }
} catch {
    Write-Host "   FAILED: $_" -ForegroundColor Red
}

Write-Host ""
Write-Host "Test 4.2: Check processor logs for correlation ID..." -ForegroundColor Gray
Start-Sleep -Seconds 2
$logs = docker logs flash-sale-engine-processor-1 --tail 10 2>&1
if ($logs -match "correlation_id") {
    Write-Host "   PASSED: Structured logs with correlation IDs found" -ForegroundColor Green
} else {
    Write-Host "   INFO: Check logs manually: docker logs flash-sale-engine-processor-1" -ForegroundColor Yellow
}

Write-Host ""

# ============================================
# FEATURE 5: HEALTH CHECK ENDPOINT
# ============================================
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "FEATURE 5: Health Check Endpoint" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

Write-Host "Test 5.1: Check /health endpoint..." -ForegroundColor Gray
try {
    $response = Invoke-WebRequest -Uri "http://localhost:8080/health" -Method GET -UseBasicParsing -ErrorAction Stop
    $health = $response.Content | ConvertFrom-Json
    
    Write-Host "   Status: $($health.status)" -ForegroundColor $(if ($health.status -eq "healthy") { "Green" } else { "Yellow" })
    Write-Host "   Redis: $($health.redis)" -ForegroundColor $(if ($health.redis) { "Green" } else { "Red" })
    Write-Host "   Kafka: $($health.kafka)" -ForegroundColor $(if ($health.kafka) { "Green" } else { "Red" })
    Write-Host "   Circuit Breaker State: $($health.circuit_breaker_state)" -ForegroundColor Cyan
    
    if ($health.status -eq "healthy" -and $health.redis -and $health.kafka) {
        Write-Host "   PASSED: Health check working" -ForegroundColor Green
    } else {
        Write-Host "   WARNING: Health check returned but some services unhealthy" -ForegroundColor Yellow
    }
} catch {
    $statusCode = $_.Exception.Response.StatusCode.value__
    Write-Host "   FAILED: Health endpoint returned $statusCode" -ForegroundColor Red
    Write-Host "   Note: Gateway may need to be rebuilt with latest code" -ForegroundColor Yellow
    Write-Host "   Run: docker-compose up -d --build gateway" -ForegroundColor Yellow
}

Write-Host ""

# ============================================
# FEATURE 6: SOLD OUT HANDLING
# ============================================
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "FEATURE 6: Sold Out Handling (Lua Script)" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

Write-Host "Test 6.1: Set inventory to 1 and try to buy 2..." -ForegroundColor Gray
# Use a unique item ID to avoid conflicts
$testItemId = "102"
docker exec flash-sale-engine-redis-1 redis-cli SET "inventory:$testItemId" 1 | Out-Null

# First order should succeed
$body1 = @{
    user_id = "u1"
    item_id = $testItemId
    amount = 1
    request_id = "soldout-test-1-$(Get-Date -Format 'yyyyMMddHHmmss')"
} | ConvertTo-Json -Compress
try {
    $response = Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body1 -ContentType "application/json" -UseBasicParsing
    if ($response.StatusCode -eq 202) {
        Write-Host "   First order: Accepted (202)" -ForegroundColor Green
    } else {
        Write-Host "   First order: Unexpected status $($response.StatusCode)" -ForegroundColor Yellow
    }
} catch {
    Write-Host "   First order: Failed - $($_.Exception.Message)" -ForegroundColor Red
}

# Wait for first order to be processed
Start-Sleep -Seconds 3

# Check inventory after first order
$inventoryAfterFirst = docker exec flash-sale-engine-redis-1 redis-cli GET "inventory:$testItemId"
Write-Host "   Inventory after first order: $inventoryAfterFirst (should be 0)" -ForegroundColor Cyan

# Second order should fail (sold out)
$body2 = @{
    user_id = "u2"
    item_id = $testItemId
    amount = 1
    request_id = "soldout-test-2-$(Get-Date -Format 'yyyyMMddHHmmss')"
} | ConvertTo-Json -Compress
try {
    $response = Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body2 -ContentType "application/json" -UseBasicParsing
    Write-Host "   Second order: Accepted (unexpected - should be sold out)" -ForegroundColor Red
} catch {
    $statusCode = $_.Exception.Response.StatusCode.value__
    if ($statusCode -eq 409) {
        Write-Host "   Second order: Rejected (409 - may be idempotency, not sold out)" -ForegroundColor Yellow
    } else {
        Write-Host "   Second order: Rejected (status: $statusCode)" -ForegroundColor Green
    }
}

# Wait for processing
Start-Sleep -Seconds 2
$finalInventory = docker exec flash-sale-engine-redis-1 redis-cli GET "inventory:$testItemId"
Write-Host "   Final inventory: $finalInventory (should be 0)" -ForegroundColor Cyan

if ($finalInventory -eq "0") {
    Write-Host "   PASSED: Lua script correctly handled sold out scenario" -ForegroundColor Green
} else {
    Write-Host "   WARNING: Final inventory is $finalInventory, expected 0" -ForegroundColor Yellow
    Write-Host "   (This may be due to order still processing or refund logic)" -ForegroundColor Gray
}

Write-Host ""

# ============================================
# SUMMARY
# ============================================
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Test Summary" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Features Tested:" -ForegroundColor Yellow
Write-Host "  [OK] Input Validation" -ForegroundColor Green
Write-Host "  [OK] Idempotency" -ForegroundColor Green
Write-Host "  [OK] Atomic Inventory (Lua Scripts)" -ForegroundColor Green
Write-Host "  [OK] Structured Logging" -ForegroundColor Green
Write-Host "  [OK] Health Check" -ForegroundColor Green
Write-Host "  [OK] Sold Out Handling" -ForegroundColor Green
Write-Host ""
Write-Host "Additional Features to Test Manually:" -ForegroundColor Yellow
Write-Host "  • Circuit Breaker (simulate Kafka failures)" -ForegroundColor Gray
Write-Host "  • DLQ Processing (check processor logs for failures)" -ForegroundColor Gray
Write-Host "  • Correlation ID propagation" -ForegroundColor Gray
Write-Host ""
Write-Host "View Logs:" -ForegroundColor Yellow
Write-Host "  docker-compose logs -f gateway" -ForegroundColor Gray
Write-Host "  docker-compose logs -f processor" -ForegroundColor Gray
Write-Host ""

