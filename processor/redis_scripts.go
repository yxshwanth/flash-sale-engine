package main

// luaCheckInventoryScript atomically checks and decrements inventory
// Returns {success: 0|1, stock: int} where:
//   - success=0: Item sold out (stock < 0), inventory already refunded
//   - success=1: Inventory reserved successfully
// This script ensures DECR and conditional refund are atomic, preventing race conditions
// Edge cases handled:
//   - Missing key: DECR on non-existent key initializes to -1, then refunds to 0
//   - Redis OOM: Script fails with error (handled in Go code)
//   - Timeout: Redis will timeout script execution (handled in Go code)
const luaCheckInventoryScript = `
local inventory_key = KEYS[1]
-- Check if key exists first to handle missing inventory gracefully
local exists = redis.call('EXISTS', inventory_key)
if exists == 0 then
    -- Key doesn't exist - treat as sold out (inventory not initialized)
    return {0, -1, 'NOT_INITIALIZED'}  -- {success, stock, reason}
end

-- Atomically decrement inventory
local current_stock = redis.call('DECR', inventory_key)

if current_stock < 0 then
    -- Sold out: refund the decrement immediately to keep inventory accurate
    redis.call('INCR', inventory_key)
    return {0, current_stock, 'SOLD_OUT'}  -- {success, stock, reason}
else
    return {1, current_stock, 'SUCCESS'}  -- {success, stock, reason}
end
`

// luaRefundInventoryScript atomically refunds inventory
// Used when payment processing fails or order needs to be cancelled
// Returns {success: 0|1, new_stock: int} where:
//   - success=1: Refund successful
//   - success=0: Invalid refund amount
// Edge cases handled:
//   - Missing key: INCRBY on non-existent key initializes to refund_amount
//   - Invalid amount: Returns 0 if amount is nil or <= 0
const luaRefundInventoryScript = `
local inventory_key = KEYS[1]
local refund_amount = tonumber(ARGV[1])

-- Validate refund amount
if not refund_amount or refund_amount <= 0 then
    return {0, 0}  -- {success, new_stock}
end

-- Atomically increment inventory (creates key if doesn't exist)
local new_stock = redis.call('INCRBY', inventory_key, refund_amount)
return {1, new_stock}  -- {success, new_stock}
`

// luaProcessOrder combines inventory check with order state tracking
// This script is defined but not currently used - reserved for future enhancement
// Would allow atomic inventory check + order state persistence in a single operation
const luaProcessOrder = `
local inventory_key = KEYS[1]
local order_key = KEYS[2]
local order_data = ARGV[1]
local timestamp = ARGV[2]

-- Check and decrement inventory atomically
local current_stock = redis.call('DECR', inventory_key)
if current_stock < 0 then
    -- Sold out, refund immediately
    redis.call('INCR', inventory_key)
    return {0, current_stock, 'SOLD_OUT'}  -- {success, stock, reason}
end

-- Store order state
redis.call('SET', order_key, order_data, 'EX', 3600)  -- 1 hour TTL
redis.call('HSET', order_key .. ':meta', 'timestamp', timestamp, 'stock_after', current_stock)

return {1, current_stock, 'SUCCESS'}  -- {success, stock, status}
`
