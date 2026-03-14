package http

import (
	"sync"
	"time"
)

const (
	maxFailedLoginAttempts = 8
	loginAttemptWindow     = 15 * time.Minute
	loginBlockDuration     = 15 * time.Minute
	loginLimiterSweepEvery = 5 * time.Minute
)

type loginRateLimiter struct {
	mu          sync.Mutex
	attempts    map[string]*loginAttemptState
	lastCleanup time.Time
}

type loginAttemptState struct {
	failed       int
	windowStart  time.Time
	blockedUntil time.Time
	lastSeen     time.Time
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{
		attempts: make(map[string]*loginAttemptState),
	}
}

func (l *loginRateLimiter) allow(ip string, now time.Time) bool {
	if ip == "" {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweepLocked(now)

	state, ok := l.attempts[ip]
	if !ok {
		return true
	}
	state.lastSeen = now

	return !now.Before(state.blockedUntil)
}

func (l *loginRateLimiter) recordFailure(ip string, now time.Time) {
	if ip == "" {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweepLocked(now)

	state, ok := l.attempts[ip]
	if !ok {
		l.attempts[ip] = &loginAttemptState{
			failed:      1,
			windowStart: now,
			lastSeen:    now,
		}
		return
	}

	state.lastSeen = now
	if now.Sub(state.windowStart) > loginAttemptWindow {
		state.failed = 0
		state.windowStart = now
	}

	state.failed++
	if state.failed >= maxFailedLoginAttempts {
		state.blockedUntil = now.Add(loginBlockDuration)
		state.failed = 0
		state.windowStart = now
	}
}

func (l *loginRateLimiter) recordSuccess(ip string) {
	if ip == "" {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.attempts, ip)
}

func (l *loginRateLimiter) sweepLocked(now time.Time) {
	if !l.lastCleanup.IsZero() && now.Sub(l.lastCleanup) < loginLimiterSweepEvery {
		return
	}

	for ip, state := range l.attempts {
		if now.Sub(state.lastSeen) > (loginAttemptWindow + loginBlockDuration) {
			delete(l.attempts, ip)
		}
	}
	l.lastCleanup = now
}
