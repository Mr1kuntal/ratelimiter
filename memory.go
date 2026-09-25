package ratelimiter

import (
	"context"
	"sync"
	"time"
)

type bucketState struct {
	mu         sync.Mutex
	tokens     float64
	lastRefill time.Time
}

// MemoryLimiter is an in-process token bucket limiter. It's accurate only
// within a single process — if your service runs multiple instances behind
// a load balancer, each instance sees a different fraction of traffic, so
// the effective limit becomes (per-instance limit x instance count). Use it
// for single-instance services, local dev, or tests; use RedisLimiter for
// anything running more than one replica.
type MemoryLimiter struct {
	cfg     Config
	buckets sync.Map // key -> *bucketState
}

// NewMemoryLimiter creates an in-process limiter with the given config.
func NewMemoryLimiter(cfg Config) *MemoryLimiter {
	return &MemoryLimiter{cfg: cfg}
}

// Allow implements Limiter.
func (m *MemoryLimiter) Allow(_ context.Context, key string) (Result, error) {
	v, _ := m.buckets.LoadOrStore(key, &bucketState{
		tokens:     float64(m.cfg.Capacity),
		lastRefill: time.Now(),
	})
	b := v.(*bucketState)

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens = minF(float64(m.cfg.Capacity), b.tokens+elapsed*m.cfg.RefillPerSecond)
	b.lastRefill = now

	allowed := b.tokens >= 1
	if allowed {
		b.tokens--
	}

	var resetAt time.Time
	if !allowed {
		secsToOneToken := (1 - b.tokens) / m.cfg.RefillPerSecond
		resetAt = now.Add(time.Duration(secsToOneToken * float64(time.Second)))
	}

	return Result{
		Allowed:   allowed,
		Remaining: int64(b.tokens),
		ResetAt:   resetAt,
	}, nil
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
