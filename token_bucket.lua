-- Atomic token bucket check-and-decrement.
--
-- KEYS[1] = bucket key
-- ARGV[1] = capacity            (max tokens / burst size)
-- ARGV[2] = refill_per_second   (steady-state refill rate)
-- ARGV[3] = now                 (unix seconds, float)
-- ARGV[4] = requested           (tokens this call consumes, normally 1)
--
-- Returns {allowed (0/1), tokens_remaining (int), reset_after_ms (int)}
--
-- Running the whole read-refill-decrement sequence as one script is what
-- makes this safe under concurrent calls from many gateway instances —
-- a plain HMGET followed by a separate HSET/MULTI would still race, since
-- two clients could both read the same starting token count before either
-- writes back.

local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

local bucket = redis.call("HMGET", key, "tokens", "last_refill")
local tokens = tonumber(bucket[1])
local last_refill = tonumber(bucket[2])

if tokens == nil then
  tokens = capacity
  last_refill = now
end

local elapsed = now - last_refill
if elapsed < 0 then
  elapsed = 0
end

tokens = math.min(capacity, tokens + elapsed * refill_rate)

local allowed = 0
if tokens >= requested then
  tokens = tokens - requested
  allowed = 1
end

redis.call("HSET", key, "tokens", tokens, "last_refill", now)
-- Let idle buckets expire instead of accumulating forever.
redis.call("EXPIRE", key, math.ceil(capacity / refill_rate) + 60)

local reset_after_ms = 0
if tokens < 1 then
  reset_after_ms = math.ceil((1 - tokens) / refill_rate * 1000)
end

return {allowed, math.floor(tokens), reset_after_ms}
