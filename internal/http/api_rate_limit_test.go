package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAPIRateLimiter_AllowsUnderLimit(t *testing.T) {
	l := newAPIRateLimiter()
	now := time.Now()

	for i := 0; i < apiRateLimitPerIP; i++ {
		if !l.allow("1.2.3.4", now) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
}

func TestAPIRateLimiter_BlocksOverLimit(t *testing.T) {
	l := newAPIRateLimiter()
	now := time.Now()

	for i := 0; i < apiRateLimitPerIP; i++ {
		l.allow("1.2.3.4", now)
	}

	if l.allow("1.2.3.4", now) {
		t.Error("request over limit should be blocked")
	}
}

func TestAPIRateLimiter_ResetsAfterWindow(t *testing.T) {
	l := newAPIRateLimiter()
	now := time.Now()

	for i := 0; i < apiRateLimitPerIP; i++ {
		l.allow("1.2.3.4", now)
	}

	// Advance past the window
	later := now.Add(apiRateLimitWindow + time.Second)
	if !l.allow("1.2.3.4", later) {
		t.Error("request should be allowed after window reset")
	}
}

func TestAPIRateLimiter_IndependentPerIP(t *testing.T) {
	l := newAPIRateLimiter()
	now := time.Now()

	// Exhaust limit for one IP
	for i := 0; i < apiRateLimitPerIP; i++ {
		l.allow("1.2.3.4", now)
	}

	// Different IP should still be allowed
	if !l.allow("5.6.7.8", now) {
		t.Error("different IP should not be affected")
	}
}

func TestAPIRateLimiter_EmptyIPAllowed(t *testing.T) {
	l := newAPIRateLimiter()
	if !l.allow("", time.Now()) {
		t.Error("empty IP should always be allowed")
	}
}

func TestAPIRateLimiter_Middleware(t *testing.T) {
	l := newAPIRateLimiter()
	called := 0
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})
	handler := l.RateLimit(inner)

	for i := 0; i < apiRateLimitPerIP+2; i++ {
		req := httptest.NewRequest("GET", "/api/stats", nil)
		req.RemoteAddr = "10.0.0.1"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if i < apiRateLimitPerIP {
			if w.Code != http.StatusOK {
				t.Fatalf("request %d: got %d, want 200", i+1, w.Code)
			}
		} else {
			if w.Code != http.StatusTooManyRequests {
				t.Fatalf("request %d: got %d, want 429", i+1, w.Code)
			}
		}
	}

	if called != apiRateLimitPerIP {
		t.Errorf("inner handler called %d times, want %d", called, apiRateLimitPerIP)
	}
}

func TestAPIRateLimiter_Sweep(t *testing.T) {
	l := newAPIRateLimiter()
	now := time.Now()

	l.allow("1.2.3.4", now)

	// Advance past sweep threshold and window
	later := now.Add(apiRateLimiterSweepEvery + apiRateLimitWindow*2 + time.Second)
	l.allow("5.6.7.8", later) // triggers sweep

	l.mu.Lock()
	_, exists := l.hits["1.2.3.4"]
	l.mu.Unlock()

	if exists {
		t.Error("stale entry should have been swept")
	}
}
