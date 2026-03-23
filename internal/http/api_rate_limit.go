package http

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	apiRateLimitPerIP        = 10 // burst size per IP
	apiRateLimitWindow       = 1 * time.Minute
	apiRateLimiterSweepEvery = 5 * time.Minute
)

type apiRateLimiter struct {
	mu          sync.Mutex
	limiters    map[string]*apiLimiterEntry
	lastCleanup time.Time
}

type apiLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newAPIRateLimiter() *apiRateLimiter {
	return &apiRateLimiter{
		limiters: make(map[string]*apiLimiterEntry),
	}
}

func (l *apiRateLimiter) limiterFor(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweepLocked(time.Now())

	entry, ok := l.limiters[ip]
	if !ok {
		// Refill rate: apiRateLimitPerIP tokens over apiRateLimitWindow.
		limiter := rate.NewLimiter(rate.Every(apiRateLimitWindow/apiRateLimitPerIP), apiRateLimitPerIP)
		l.limiters[ip] = &apiLimiterEntry{limiter: limiter, lastSeen: time.Now()}
		return limiter
	}

	entry.lastSeen = time.Now()
	return entry.limiter
}

func (l *apiRateLimiter) sweepLocked(now time.Time) {
	if !l.lastCleanup.IsZero() && now.Sub(l.lastCleanup) < apiRateLimiterSweepEvery {
		return
	}

	for ip, entry := range l.limiters {
		if now.Sub(entry.lastSeen) > apiRateLimitWindow*2 {
			delete(l.limiters, ip)
		}
	}
	l.lastCleanup = now
}

// RateLimit wraps a handler and rejects requests that exceed the per-IP limit.
func (l *apiRateLimiter) RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if ip != "" && !l.limiterFor(ip).Allow() {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
