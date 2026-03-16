package api

import (
	"sync"
	"time"
)

// rateLimiter implements a simple per-key token bucket with stale entry eviction.
type rateLimiter struct {
	mu            sync.Mutex
	buckets       map[string]*bucket
	rate          int
	stop          chan struct{}
	evictInterval time.Duration
}

type bucket struct {
	tokens   int
	lastFill time.Time
	lastSeen time.Time
}

func newRateLimiter(requestsPerMinute int) *rateLimiter {
	rl := &rateLimiter{
		buckets:       make(map[string]*bucket),
		rate:          requestsPerMinute,
		stop:          make(chan struct{}),
		evictInterval: 5 * time.Minute,
	}
	go rl.evictLoop()
	return rl
}

// Close stops the eviction goroutine.
func (rl *rateLimiter) Close() {
	close(rl.stop)
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		rl.buckets[key] = &bucket{tokens: rl.rate - 1, lastFill: now, lastSeen: now}
		return true
	}

	b.lastSeen = now

	// Refill tokens based on time since last refill
	elapsed := now.Sub(b.lastFill)
	refill := int(elapsed.Minutes() * float64(rl.rate))
	if refill > 0 {
		b.tokens += refill
		if b.tokens > rl.rate {
			b.tokens = rl.rate
		}
		b.lastFill = now
	}

	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}

// evictLoop removes stale entries periodically.
func (rl *rateLimiter) evictLoop() {
	ticker := time.NewTicker(rl.evictInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			cutoff := time.Now().Add(-10 * time.Minute)
			for k, b := range rl.buckets {
				if b.lastSeen.Before(cutoff) {
					delete(rl.buckets, k)
				}
			}
			rl.mu.Unlock()
		case <-rl.stop:
			return
		}
	}
}
