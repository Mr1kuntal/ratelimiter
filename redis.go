package ratelimiter

import (
	"context"
	_ "embed"
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
func NewRedisLimiter(rdb redis.Cmdable, cfg Config, opts ...RedisOption) *RedisLimiter {
	r := &RedisLimiter{
		rdb:    rdb,
		cfg:    cfg,
		script: redis.NewScript(tokenBucketScript),
		prefix: "ratelimit:",
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Allow implements Limiter. It calls the token bucket Lua script, so the
// check-and-decrement is atomic even under concurrent calls from other
// instances hitting the same key.
func (r *RedisLimiter) Allow(ctx context.Context, key string) (Result, error) {
	now := float64(time.Now().UnixNano()) / 1e9

	res, err := r.script.Run(ctx, r.rdb, []string{r.prefix + key},
		r.cfg.Capacity, r.cfg.RefillPerSecond, now, 1,
	).Result()
	if err != nil {
		return Result{}, err
	}

	vals := res.([]interface{})
	allowed := vals[0].(int64) == 1
	remaining := vals[1].(int64)
	resetAfterMs := vals[2].(int64)

	return Result{
		Allowed:   allowed,
		Remaining: remaining,
		ResetAt:   time.Now().Add(time.Duration(resetAfterMs) * time.Millisecond),
	}, nil
}