package ratelimiter

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// KeyFunc extracts the rate-limit key from an incoming request — e.g. an
// authenticated user ID from context, an API key header, or the client IP.
type KeyFunc func(r *http.Request) string

// ByIP is a KeyFunc that keys on the connecting IP address (from
// r.RemoteAddr), ignoring any client-supplied headers. This is the safe
// default: an HTTP header can be set by anyone sending the request, so
// trusting one for rate-limit identity is only safe behind infrastructure
// you control that strips or overwrites it — see ByForwardedFor for that
// case.
func ByIP(r *http.Request) string {
	// r.RemoteAddr is "ip:port" — the port is the ephemeral client-side
	// TCP port and differs on every connection, so it must be stripped or
	// every request lands in its own bucket. Fall back to the raw value
	// if it's ever in an unexpected format (missing port, IPv6 without
	// brackets, etc.) rather than erroring the request.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ByForwardedFor is a KeyFunc that trusts the X-Forwarded-For header,
// taking its left-most address.
//
// WARNING: only use this if your service sits behind a proxy/load
// balancer you control that overwrites this header (rather than blindly
// appending to whatever arrives) before forwarding the request, AND
// clients cannot reach your service directly. Otherwise this is trivially
// spoofable: a client can send a different X-Forwarded-For on every
// request and bypass rate limiting entirely. If you're not certain your
// deployment satisfies this, use ByIP instead.
func ByForwardedFor(r *http.Request) string {
	fwd := r.Header.Get("X-Forwarded-For")
	if fwd == "" {
		return ByIP(r)
	}
	first, _, _ := strings.Cut(fwd, ",")
	return strings.TrimSpace(first)
}

// MiddlewareOption configures Middleware.
type MiddlewareOption func(*middlewareConfig)

type middlewareConfig struct {
	failOpen bool
}

// WithFailClosed rejects requests (HTTP 503) when the limiter itself
// errors (e.g. Redis is unreachable), instead of the default fail-open
// behavior of letting the request through.
func WithFailClosed() MiddlewareOption {
	return func(c *middlewareConfig) { c.failOpen = false }
}

// Middleware wraps an http.Handler with rate limiting. Requests that fail
// the check get HTTP 429 with the standard rate-limit headers. By default,
// requests that hit a limiter error (e.g. Redis unavailable) are allowed
// through — pass WithFailClosed() if your system should reject instead.
func Middleware(limiter Limiter, keyFn KeyFunc, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	cfg := middlewareConfig{failOpen: true}
	for _, opt := range opts {
		opt(&cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result, err := limiter.Allow(r.Context(), keyFn(r))
			if err != nil {
				if cfg.failOpen {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, `{"error":"rate limiter unavailable"}`, http.StatusServiceUnavailable)
				return
			}

			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))

			if !result.Allowed {
				retrySeconds := int64(math.Ceil(result.RetryAfter.Seconds()))
				w.Header().Set("Retry-After", strconv.FormatInt(retrySeconds, 10))
				// X-RateLimit-Reset is conventionally an absolute unix
				// timestamp for client display; deriving it from the
				// caller's wall clock here is fine since this header is
				// informational only, not part of the limiter's own
				// correctness (which never depends on this value).
				w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(result.RetryAfter).Unix(), 10))
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}