// Package ratelimiter provides a reusable token-bucket rate limiter with
// two interchangeable backends:
//
//   - MemoryLimiter: in-process, for single-instance services or tests.
//   - RedisLimiter:  distributed, safe across multiple instances/gateways,
//     using a Lua script so the read-refill-decrement sequence is atomic.
//
// Both implement the same Limiter interface, so callers (e.g. HTTP
// middleware) don't need to know which backend is in use.
package ratelimiter

import (
	"context"
	"time"
)

// Result describes the outcome of a single rate limit check.
type Result struct {
	// Allowed reports whether the request may proceed.
	Allowed bool
	// Remaining is the number of tokens left in the bucket after this check.
	Remaining int64
	// ResetAt is when the bucket is expected to have at least one token
	// available again (only meaningful when Allowed is false).
	ResetAt time.Time
}

// Limiter checks whether a request identified by key should be permitted.
// Each call consumes exactly one token.
type Limiter interface {
	Allow(ctx context.Context, key string) (Result, error)
}

// Config holds token bucket parameters, shared by every backend.
type Config struct {
	// Capacity is the maximum number of tokens the bucket holds — i.e. the
	// largest burst a client can make before being throttled.
	Capacity int64
	// RefillPerSecond is the steady-state number of tokens added per second.
	RefillPerSecond float64
}
