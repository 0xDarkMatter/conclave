package batch

import (
	"context"
	"sync"
	"time"
)

// RateLimiter provides token bucket rate limiting for batch processing
type RateLimiter struct {
	interval      time.Duration
	baseInterval  time.Duration // Original interval for reference
	mu            sync.Mutex
	lastTime      time.Time
	consecutiveOK int // Track success streak for adaptive speedup
}

// NewRateLimiter creates a rate limiter based on provider count
// Uses conservative defaults to avoid 429 errors:
// - 1 provider:   60 RPM (1s interval) - single provider can go faster
// - 2 providers:  20 RPM per request (3s interval)
// - 3-4 providers: 15 RPM per request (4s interval)
// - 5+ providers:  10 RPM per request (6s interval)
func NewRateLimiter(providerCount int) *RateLimiter {
	var interval time.Duration
	switch {
	case providerCount == 1:
		interval = 1 * time.Second // Single provider: much faster
	case providerCount == 2:
		interval = 3 * time.Second
	case providerCount <= 4:
		interval = 4 * time.Second
	default:
		interval = 6 * time.Second
	}

	return &RateLimiter{
		interval:     interval,
		baseInterval: interval,
		lastTime:     time.Time{},
	}
}

// NewUnlimitedRateLimiter creates a rate limiter with no delay (for high-tier accounts)
func NewUnlimitedRateLimiter() *RateLimiter {
	return &RateLimiter{
		interval: 0,
		lastTime: time.Time{},
	}
}

// Wait blocks until the rate limit allows the next request
func (r *RateLimiter) Wait(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if !r.lastTime.IsZero() {
		elapsed := now.Sub(r.lastTime)
		if elapsed < r.interval {
			wait := r.interval - elapsed
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	r.lastTime = time.Now()
	return nil
}

// SetInterval allows dynamic adjustment of the rate limit
func (r *RateLimiter) SetInterval(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interval = d
}

// RecordSuccess tracks successful requests for adaptive speedup
// After 10 consecutive successes, speeds up the rate limiter
func (r *RateLimiter) RecordSuccess() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.consecutiveOK++
	if r.consecutiveOK >= 10 {
		r.consecutiveOK = 0
		r.speedUpLocked()
	}
}

// RecordRateLimit slows down on 429 errors
func (r *RateLimiter) RecordRateLimit() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.consecutiveOK = 0
	r.slowDownLocked()
}

// slowDownLocked increases the interval by 50% (must hold lock)
func (r *RateLimiter) slowDownLocked() {
	r.interval = r.interval * 3 / 2
	// Cap at 30 seconds
	if r.interval > 30*time.Second {
		r.interval = 30 * time.Second
	}
}

// speedUpLocked decreases the interval by 25% (must hold lock)
func (r *RateLimiter) speedUpLocked() {
	r.interval = r.interval * 3 / 4
	// Floor at base interval (don't go faster than original)
	if r.interval < r.baseInterval {
		r.interval = r.baseInterval
	}
}

// Interval returns the current rate limit interval
func (r *RateLimiter) Interval() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.interval
}
