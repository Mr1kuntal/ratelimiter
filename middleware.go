package ratelimiter

import (
	"net/http"
	"strconv"
	"time"
)

// KeyFunc extracts the rate-limit key from an incoming request — e.g. an
// authenticated user ID from context, an API key header, or the client IP.
type KeyFunc func(r *http.Request) string

// ByIP is a KeyFunc that keys on the client's IP address. If you're behind
// a trusted proxy/load balancer, prefer parsing X-Forwarded-For yourself;
// this is a simple starting point, not hardened against spoofing.
func ByIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return fwd
	}
	return r.RemoteAddr
}

// Middleware wraps an http.Handler with rate limiting. Requests that fail
// the check get HTTP 429 with the standard rate-limit headers; requests
// that hit a limiter error (e.g. Redis unavailable) are allowed through —
// this middleware fails open. Swap that behavior if your system should
// fail closed instead.
func Middleware(limiter Limiter, keyFn KeyFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result, err := limiter.Allow(r.Context(), keyFn(r))
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))

			if !result.Allowed {
				w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetAt.Unix(), 10))
				w.Header().Set("Retry-After", strconv.FormatInt(int64(time.Until(result.ResetAt).Seconds())+1, 10))
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
