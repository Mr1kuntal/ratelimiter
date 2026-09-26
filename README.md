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
limiter, err := ratelimiter.NewMemoryLimiter(ratelimiter.Config{
    Capacity:        100, // burst size
    RefillPerSecond: 10,  // steady-state rate
})
if err != nil {
    log.Fatal(err)
}

result, err := limiter.Allow(ctx, "user:123")
if err == nil && !result.Allowed {
    // reject: result.RetryAfter tells you how long until a token is available again
}
```

## Quick start — distributed (Redis)

```go
rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})

limiter, err := ratelimiter.NewRedisLimiter(rdb, ratelimiter.Config{
    Capacity:        100,
    RefillPerSecond: 10,
}, ratelimiter.WithKeyPrefix("myapp:ratelimit:"))
if err != nil {
    log.Fatal(err)
}

result, err := limiter.Allow(ctx, "user:123")
```

## As net/http middleware

```go
limiter, err := ratelimiter.NewRedisLimiter(rdb, ratelimiter.Config{
    Capacity:        100,
    RefillPerSecond: 10,
})
if err != nil {
    log.Fatal(err)
}

mux := http.NewServeMux()
mux.HandleFunc("/api/", apiHandler)

// Default is fail-open (a Redis outage doesn't take the API down).
// Pass ratelimiter.WithFailClosed() to reject instead when the limiter errors.
handler := ratelimiter.Middleware(limiter, ratelimiter.ByIP)(mux)
http.ListenAndServe(":8080", handler)
```

Swap `ratelimiter.ByIP` for your own `KeyFunc` to key on an authenticated
user ID (e.g. pulled from a JWT already validated by your auth middleware)
instead of IP. `ByIP` uses the connecting socket's address and ignores any
client-supplied headers — the safe default. If you're deployed behind a
proxy/load balancer you control that overwrites `X-Forwarded-For` (not one
that blindly appends to it, and one clients can't bypass), `ByForwardedFor`
is available instead; read its doc comment before using it, since getting
this wrong makes rate limiting trivially bypassable.

## Notes

- One `Config` = one rule. Layer multiple `Limiter`s (per-user, per-IP,
  per-endpoint) and enforce the most restrictive result if you need more
  than one rule at once.
- `MemoryLimiter` only sees traffic that hits that one process — fine for
  a single-instance service, but each replica behind a load balancer would
  enforce the limit independently once you scale out. Move to
  `RedisLimiter` at that point.
- The Redis backend fails closed by default (an error from `Allow` is
  returned, not silently treated as "allowed"); `Middleware` defaults to
  failing *open* on that error so a Redis outage doesn't take your API
  down — pass `WithFailClosed()` if you want the opposite.
- Config is validated at construction (`Capacity` and `RefillPerSecond`
  must both be > 0) — both constructors return an error instead of a
  limiter you'd only find broken at request time.
- `Allow(ctx, "")` returns `ErrEmptyKey` rather than silently rate-limiting
  under a single shared global bucket.