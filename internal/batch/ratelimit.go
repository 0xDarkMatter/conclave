package batch

import (
	"context"
	"sync"
	"time"
)

// RateLimiter provides token bucket rate limiting for batch processing
type RateLimiter struct {
	interval time.Duration
	mu       sync.Mutex
	lastTime time.Time
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
		interval: interval,
		lastTime: time.Time{},
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

// SlowDown increases the interval by 50% (for adaptive backoff)
func (r *RateLimiter) SlowDown() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interval = r.interval * 3 / 2
	// Cap at 30 seconds
	if r.interval > 30*time.Second {
		r.interval = 30 * time.Second
	}
}

// SpeedUp decreases the interval by 25% (for adaptive recovery)
func (r *RateLimiter) SpeedUp() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interval = r.interval * 3 / 4
	// Floor at 1 second
	if r.interval < time.Second {
		r.interval = time.Second
	}
}

// Interval returns the current rate limit interval
func (r *RateLimiter) Interval() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.interval
}
