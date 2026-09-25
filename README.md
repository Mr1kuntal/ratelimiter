# ratelimiter

A small, reusable token bucket rate limiter for Go services. Two backends
share one interface, so you can start in-process and move to Redis later
without touching call sites:

- `MemoryLimiter` — in-process, single instance, zero dependencies.
- `RedisLimiter` — distributed, atomic via a Lua script, safe across many
  instances/gateways sharing the same Redis.

## Install

Update the module path in `go.mod` to your own repo, then:

```
go get github.com/redis/go-redis/v9
```

## Quick start — single instance / local dev

```go
limiter := ratelimiter.NewMemoryLimiter(ratelimiter.Config{
    Capacity:        100, // burst size
    RefillPerSecond: 10,  // steady-state rate
})

result, err := limiter.Allow(ctx, "user:123")
if err == nil && !result.Allowed {
    // reject: result.ResetAt tells you when a token will be available again
}
```

## Quick start — distributed (Redis)

```go
rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})

limiter := ratelimiter.NewRedisLimiter(rdb, ratelimiter.Config{
    Capacity:        100,
    RefillPerSecond: 10,
}, ratelimiter.WithKeyPrefix("myapp:ratelimit:"))

result, err := limiter.Allow(ctx, "user:123")
```

## As net/http middleware

```go
limiter := ratelimiter.NewRedisLimiter(rdb, ratelimiter.Config{
    Capacity:        100,
    RefillPerSecond: 10,
})

mux := http.NewServeMux()
mux.HandleFunc("/api/", apiHandler)

handler := ratelimiter.Middleware(limiter, ratelimiter.ByIP)(mux)
http.ListenAndServe(":8080", handler)
```

Swap `ratelimiter.ByIP` for your own `KeyFunc` to key on an authenticated
user ID (e.g. pulled from a JWT already validated by your auth middleware)
instead of IP.

## Notes

- One `Config` = one rule. Layer multiple `Limiter`s (per-user, per-IP,
  per-endpoint) and enforce the most restrictive result if you need more
  than one rule at once.
- `MemoryLimiter` only sees traffic that hits that one process — fine for
  a single-instance service, but each replica behind a load balancer would
  enforce the limit independently once you scale out. Move to
  `RedisLimiter` at that point.
- The Redis backend fails closed by default (an error from `Allow` is
  returned, not silently treated as "allowed"); the provided HTTP
  middleware chooses to fail *open* on that error so a Redis outage
  doesn't take your API down — pick whichever fits your system.
