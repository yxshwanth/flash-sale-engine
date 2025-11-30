# Quick test script for sending buy requests

param(
    [string]$UserId = "u1",
    [string]$ItemId = "101",
    [int]$Amount = 1,
    [string]$RequestId = "req-$(Get-Date -Format 'yyyyMMddHHmmss')"
)

$body = @{
    user_id = $UserId
    item_id = $ItemId
    amount = $Amount
    request_id = $RequestId
} | ConvertTo-Json

Write-Host "Sending order request..." -ForegroundColor Yellow
Write-Host "  User ID: $UserId" -ForegroundColor Gray
Write-Host "  Item ID: $ItemId" -ForegroundColor Gray
Write-Host "  Amount: $Amount" -ForegroundColor Gray
Write-Host "  Request ID: $RequestId" -ForegroundColor Gray
Write-Host ""

try {
    $response = Invoke-WebRequest -Uri "http://localhost:8080/buy" -Method POST -Body $body -ContentType "application/json" -UseBasicParsing
    Write-Host "Status: $($response.StatusCode)" -ForegroundColor Green
    Write-Host "Response: $($response.Content)" -ForegroundColor Cyan
} catch {
    $statusCode = $_.Exception.Response.StatusCode.value__
    Write-Host "Status: $statusCode" -ForegroundColor $(if ($statusCode -eq 409) { "Yellow" } else { "Red" })
    Write-Host "Error: $_" -ForegroundColor Red
}

