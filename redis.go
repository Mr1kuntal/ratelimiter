package ratelimiter

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

//go:embed token_bucket.lua
var tokenBucketScript string

// RedisLimiter is a distributed token bucket limiter backed by Redis. Every
// gateway/instance sharing the same Redis (or Redis Cluster) sees the same
// bucket state, so limits are enforced globally regardless of which
// instance a request lands on.
type RedisLimiter struct {
	rdb    redis.Cmdable
	cfg    Config
	script *redis.Script
	prefix string
}

// RedisOption configures a RedisLimiter.
type RedisOption func(*RedisLimiter)

// WithKeyPrefix namespaces every bucket key, e.g. "ratelimit:". Useful to
// avoid collisions if you share a Redis instance across services.
func WithKeyPrefix(prefix string) RedisOption {
	return func(r *RedisLimiter) { r.prefix = prefix }
}

// NewRedisLimiter creates a distributed limiter using rdb for storage.
// rdb can be a *redis.Client (single instance) or a *redis.ClusterClient
// (Redis Cluster) — both satisfy redis.Cmdable, and the Lua script runs
// unchanged either way since Cluster routes each key to its shard.
func NewRedisLimiter(rdb redis.Cmdable, cfg Config, opts ...RedisOption) (*RedisLimiter, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	r := &RedisLimiter{
		rdb:    rdb,
		cfg:    cfg,
		script: redis.NewScript(tokenBucketScript),
		prefix: "ratelimit:",
	}
	for _, opt := range opts {
		opt(r)
	}
	return r, nil
}

// Allow implements Limiter. It calls the token bucket Lua script, so the
// check-and-decrement is atomic even under concurrent calls from other
// instances hitting the same key. The current time is read inside the
// script from Redis itself, so clock drift between app servers can't skew
// refill rates.
func (r *RedisLimiter) Allow(ctx context.Context, key string) (Result, error) {
	if key == "" {
		return Result{}, ErrEmptyKey
	}

	res, err := r.script.Run(ctx, r.rdb, []string{r.prefix + key},
		r.cfg.Capacity, r.cfg.RefillPerSecond, 1,
	).Result()
	if err != nil {
		return Result{}, err
	}

	vals, ok := res.([]interface{})
	if !ok || len(vals) != 3 {
		return Result{}, fmt.Errorf("ratelimiter: unexpected script result shape: %#v", res)
	}

	allowedVal, ok1 := vals[0].(int64)
	remaining, ok2 := vals[1].(int64)
	resetAfterMs, ok3 := vals[2].(int64)
	if !ok1 || !ok2 || !ok3 {
		return Result{}, fmt.Errorf("ratelimiter: unexpected script result types: %#v", vals)
	}

	return Result{
		Allowed:    allowedVal == 1,
		Remaining:  remaining,
		RetryAfter: time.Duration(resetAfterMs) * time.Millisecond,
	}, nil
}