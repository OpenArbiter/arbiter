package github

import (
	"log/slog"
	"net/http"

	"golang.org/x/time/rate"
)

// RateLimiter provides global rate limiting for webhook requests.
type RateLimiter struct {
	global *rate.Limiter
}

// NewRateLimiter creates a rate limiter with the given requests per second and burst size.
func NewRateLimiter(rps float64, burst int) *RateLimiter {
	return &RateLimiter{
		global: rate.NewLimiter(rate.Limit(rps), burst),
	}
}

// Middleware wraps an http.Handler with rate limiting.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.global.Allow() {
			slog.Warn("rate limit exceeded",
				"remote_addr", r.RemoteAddr,
			)
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
