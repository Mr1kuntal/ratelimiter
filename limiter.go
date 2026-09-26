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
	"errors"
	"fmt"
	"time"
)

// ErrEmptyKey is returned by Allow when called with an empty key. Silently
// allowing this would mean every caller that forgets to supply a key (e.g.
// an unauthenticated request reaching a user-ID KeyFunc) shares a single
// global bucket — a subtle and dangerous bug in a reusable package.
var ErrEmptyKey = errors.New("ratelimiter: key must not be empty")

// Result describes the outcome of a single rate limit check.
type Result struct {
	// Allowed reports whether the request may proceed.
	Allowed bool
	// Remaining is the number of tokens left in the bucket after this check.
	Remaining int64
	// RetryAfter is how long to wait before at least one token is expected
	// to be available again (zero when Allowed is true). It's a duration,
	// not a timestamp: a token bucket refills continuously rather than
	// "resetting" at a fixed point, and a duration computed server-side
	// avoids depending on the caller's wall clock — deriving an absolute
	// time is a one-line time.Now().Add(result.RetryAfter) for callers
	// that want it, but that conversion is on them, not baked into the
	// package's own correctness.
	RetryAfter time.Duration
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

func (c Config) validate() error {
	if c.Capacity <= 0 {
		return fmt.Errorf("ratelimiter: Capacity must be > 0, got %d", c.Capacity)
	}
	if c.RefillPerSecond <= 0 {
		return fmt.Errorf("ratelimiter: RefillPerSecond must be > 0, got %v", c.RefillPerSecond)
	}
	return nil
}