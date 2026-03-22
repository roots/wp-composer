package http

import (
	"net/http"
	"sync"
	"time"
)

const (
	apiRateLimitPerIP = 10            // requests per window per IP
	apiRateLimitWindow = 1 * time.Minute
	apiRateLimiterSweepEvery = 5 * time.Minute
)

type apiRateLimiter struct {
	mu          sync.Mutex
	hits        map[string]*apiHitState
	lastCleanup time.Time
}

type apiHitState struct {
	count       int
	windowStart time.Time
	lastSeen    time.Time
}

func newAPIRateLimiter() *apiRateLimiter {
	return &apiRateLimiter{
		hits: make(map[string]*apiHitState),
	}
}

// allow checks whether the IP is within its rate limit and, if so, increments
// the counter. Returns true when the request should proceed.
func (l *apiRateLimiter) allow(ip string, now time.Time) bool {
	if ip == "" {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweepLocked(now)

	state, ok := l.hits[ip]
	if !ok {
		l.hits[ip] = &apiHitState{
			count:       1,
			windowStart: now,
			lastSeen:    now,
		}
		return true
	}

	state.lastSeen = now

	// Reset window if it has elapsed.
	if now.Sub(state.windowStart) > apiRateLimitWindow {
		state.count = 1
		state.windowStart = now
		return true
	}

	if state.count >= apiRateLimitPerIP {
		return false
	}

	state.count++
	return true
}

func (l *apiRateLimiter) sweepLocked(now time.Time) {
	if !l.lastCleanup.IsZero() && now.Sub(l.lastCleanup) < apiRateLimiterSweepEvery {
		return
	}

	for ip, state := range l.hits {
		if now.Sub(state.lastSeen) > apiRateLimitWindow*2 {
			delete(l.hits, ip)
		}
	}
	l.lastCleanup = now
}

// RateLimit wraps a handler and rejects requests that exceed the per-IP limit.
func (l *apiRateLimiter) RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if !l.allow(ip, time.Now()) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
