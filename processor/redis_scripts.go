package main

// luaCheckInventoryScript atomically checks and decrements inventory
// Returns {success: 0|1, stock: int} where:
//   - success=0: Item sold out (stock < 0), inventory already refunded
//   - success=1: Inventory reserved successfully
// This script ensures DECR and conditional refund are atomic, preventing race conditions
const luaCheckInventoryScript = `
local inventory_key = KEYS[1]
local current_stock = redis.call('DECR', inventory_key)
if current_stock < 0 then
    -- Sold out: refund the decrement immediately to keep inventory accurate
    redis.call('INCR', inventory_key)
    return {0, current_stock}  -- {success, stock}
else
    return {1, current_stock}  -- {success, stock}
end
`

// luaRefundInventoryScript atomically refunds inventory
// Used when payment processing fails or order needs to be cancelled
// Returns 1 on success, 0 if refund_amount is invalid
const luaRefundInventoryScript = `
local inventory_key = KEYS[1]
local refund_amount = tonumber(ARGV[1])
if refund_amount and refund_amount > 0 then
    redis.call('INCRBY', inventory_key, refund_amount)
    return 1
else
    return 0
end
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

