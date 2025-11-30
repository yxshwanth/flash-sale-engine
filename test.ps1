# Flash Sale Engine Test Script

Write-Host "=== Flash Sale Engine Test ===" -ForegroundColor Cyan
Write-Host ""

# Test 1: Check if services are running
Write-Host "1. Checking if services are running..." -ForegroundColor Yellow
$gatewayRunning = docker ps --filter "name=flash-sale-engine-gateway" --format "{{.Names}}" | Select-String "gateway"
if ($gatewayRunning) {
    Write-Host "   Gateway is running" -ForegroundColor Green
} else {
    Write-Host "   Gateway is not running" -ForegroundColor Red
    Write-Host "   Run: docker-compose up -d" -ForegroundColor Yellow
    exit 1
}

# Test 2: Seed inventory
Write-Host ""
Write-Host "2. Seeding inventory..." -ForegroundColor Yellow
docker exec flash-sale-engine-redis-1 redis-cli SET inventory:101 100 | Out-Null
if ($LASTEXITCODE -eq 0) {
    Write-Host "   Inventory seeded" -ForegroundColor Green
} else {
    Write-Host "   Failed to seed inventory" -ForegroundColor Red
}

# Test 3: Test idempotency (send same request twice)
Write-Host ""
Write-Host "3. Testing Idempotency..." -ForegroundColor Yellow

$requestBody = @{
    user_id = "u1"
    item_id = "101"
    amount = 1
    request_id = "test-req-123"
} | ConvertTo-Json

Write-Host "   First request..." -ForegroundColor Gray
try {
    $response1 = Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $requestBody -ContentType "application/json" -UseBasicParsing
    Write-Host "   Status: $($response1.StatusCode) - $($response1.Content)" -ForegroundColor Green
} catch {
    Write-Host "   First request failed: $_" -ForegroundColor Red
}

Write-Host "   Second request with same request_id..." -ForegroundColor Gray
try {
    $response2 = Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $requestBody -ContentType "application/json" -UseBasicParsing -ErrorAction Stop
    Write-Host "   Status: $($response2.StatusCode)" -ForegroundColor $(if ($response2.StatusCode -eq 409) { "Green" } else { "Red" })
    if ($response2.StatusCode -eq 409) {
        Write-Host "   Idempotency working! Duplicate request rejected." -ForegroundColor Green
    }
} catch {
    $statusCode = $_.Exception.Response.StatusCode.value__
    if ($statusCode -eq 409) {
        Write-Host "   Status: 409 - Duplicate Request Detected" -ForegroundColor Green
        Write-Host "   Idempotency working! Duplicate request rejected." -ForegroundColor Green
    } else {
        Write-Host "   Unexpected error: $_" -ForegroundColor Red
    }
}

# Test 4: Send multiple orders
Write-Host ""
Write-Host "4. Sending 5 orders..." -ForegroundColor Yellow
for ($i = 1; $i -le 5; $i++) {
    $orderBody = @{
        user_id = "u$i"
        item_id = "101"
        amount = 1
        request_id = "req-$i-$(Get-Date -Format 'yyyyMMddHHmmss')"
    } | ConvertTo-Json
    
    try {
        $response = Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $orderBody -ContentType "application/json" -UseBasicParsing
        Write-Host "   Order $i : Status $($response.StatusCode)" -ForegroundColor Green
    } catch {
        Write-Host "   Order $i : Failed" -ForegroundColor Red
    }
    Start-Sleep -Milliseconds 200
}

# Test 5: Check inventory
Write-Host ""
Write-Host "5. Checking remaining inventory..." -ForegroundColor Yellow
$inventory = docker exec flash-sale-engine-redis-1 redis-cli GET inventory:101
Write-Host "   Remaining inventory: $inventory" -ForegroundColor Cyan

# Test 6: Check processor logs
Write-Host ""
Write-Host "6. Checking processor logs..." -ForegroundColor Yellow
docker logs flash-sale-engine-processor-1 --tail 10

Write-Host ""
Write-Host "=== Test Complete ===" -ForegroundColor Cyan
Write-Host ""
Write-Host "To view all logs:" -ForegroundColor Yellow
Write-Host "  docker-compose logs -f processor" -ForegroundColor Gray
Write-Host "  docker-compose logs -f gateway" -ForegroundColor Gray
