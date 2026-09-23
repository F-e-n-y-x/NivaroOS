package main

import (
	"context"
	"sync"
	"time"
)

// rateLimiter is a token bucket shared by every connection of every
// download, so the global speed limit holds no matter how many segments
// are running. Rate 0 means unlimited.
type rateLimiter struct {
	mu     sync.Mutex
	rate   int64
	tokens float64
	last   time.Time
}

func newRateLimiter(rate int64) *rateLimiter {
	return &rateLimiter{rate: rate, last: time.Now()}
}

func (l *rateLimiter) SetRate(rate int64) {
	l.mu.Lock()
	l.rate = rate
	l.tokens = 0
	l.last = time.Now()
	l.mu.Unlock()
}

func (l *rateLimiter) Wait(ctx context.Context, n int) error {
	for {
		l.mu.Lock()
		if l.rate <= 0 {
			l.mu.Unlock()
			return nil
		}
		now := time.Now()
		l.tokens += now.Sub(l.last).Seconds() * float64(l.rate)
		l.last = now
		// Burst of one second's worth, but never less than one read, or a
		// tiny limit could never admit a full buffer.
		burst := float64(l.rate)
		if burst < float64(n) {
			burst = float64(n)
		}
		if l.tokens > burst {
			l.tokens = burst
		}
		if l.tokens >= float64(n) {
			l.tokens -= float64(n)
			l.mu.Unlock()
			return nil
		}
		wait := time.Duration((float64(n) - l.tokens) / float64(l.rate) * float64(time.Second))
		l.mu.Unlock()
		if wait > 250*time.Millisecond {
			wait = 250 * time.Millisecond
		}
		if !sleepCtx(ctx, wait) {
			return ctx.Err()
		}
	}
}
