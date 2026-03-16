package api

import (
	"testing"
	"time"
)

func TestRateLimiter_AllowsBurst(t *testing.T) {
	rl := newRateLimiter(5)
	defer rl.Close()

	for i := 0; i < 5; i++ {
		if !rl.allow("key1") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}
}

func TestRateLimiter_DeniesOverBurst(t *testing.T) {
	rl := newRateLimiter(3)
	defer rl.Close()

	for i := 0; i < 3; i++ {
		rl.allow("key1")
	}
	if rl.allow("key1") {
		t.Error("4th request should be denied")
	}
}

func TestRateLimiter_IndependentKeys(t *testing.T) {
	rl := newRateLimiter(2)
	defer rl.Close()

	rl.allow("a")
	rl.allow("a")
	if rl.allow("a") {
		t.Error("key a should be exhausted")
	}
	if !rl.allow("b") {
		t.Error("key b should still be allowed")
	}
}

func TestRateLimiter_RefillsAfterTime(t *testing.T) {
	rl := newRateLimiter(60) // 60/min = 1/sec
	defer rl.Close()

	// Exhaust tokens
	for i := 0; i < 60; i++ {
		rl.allow("key1")
	}
	if rl.allow("key1") {
		t.Error("should be exhausted")
	}

	// Manually advance lastFill to simulate time passing
	rl.mu.Lock()
	rl.buckets["key1"].lastFill = time.Now().Add(-2 * time.Minute)
	rl.mu.Unlock()

	if !rl.allow("key1") {
		t.Error("should be allowed after refill")
	}
}

func TestRateLimiter_EvictLoop(t *testing.T) {
	rl := &rateLimiter{
		buckets: make(map[string]*bucket),
		rate:    10,
		stop:    make(chan struct{}),
	}

	// Add a stale entry and a fresh entry
	now := time.Now()
	rl.buckets["stale"] = &bucket{tokens: 5, lastFill: now, lastSeen: now.Add(-15 * time.Minute)}
	rl.buckets["fresh"] = &bucket{tokens: 5, lastFill: now, lastSeen: now}

	// Manually run eviction logic (same as evictLoop body)
	rl.mu.Lock()
	cutoff := now.Add(-10 * time.Minute)
	for k, b := range rl.buckets {
		if b.lastSeen.Before(cutoff) {
			delete(rl.buckets, k)
		}
	}
	rl.mu.Unlock()

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if _, ok := rl.buckets["stale"]; ok {
		t.Error("stale entry should have been evicted")
	}
	if _, ok := rl.buckets["fresh"]; !ok {
		t.Error("fresh entry should still exist")
	}
}

func TestRateLimiter_EvictLoopGoroutine(t *testing.T) {
	// Create a rate limiter with a very short eviction interval
	rl := &rateLimiter{
		buckets:       make(map[string]*bucket),
		rate:          10,
		stop:          make(chan struct{}),
		evictInterval: 10 * time.Millisecond,
	}

	now := time.Now()
	rl.buckets["stale"] = &bucket{tokens: 5, lastFill: now, lastSeen: now.Add(-15 * time.Minute)}
	rl.buckets["fresh"] = &bucket{tokens: 5, lastFill: now, lastSeen: now}

	go rl.evictLoop()

	// Wait for eviction to run
	time.Sleep(50 * time.Millisecond)
	rl.Close()

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if _, ok := rl.buckets["stale"]; ok {
		t.Error("stale entry should have been evicted by evictLoop goroutine")
	}
	if _, ok := rl.buckets["fresh"]; !ok {
		t.Error("fresh entry should still exist")
	}
}

func TestRateLimiter_Close(t *testing.T) {
	rl := newRateLimiter(10)
	rl.Close()
	// Closing should not panic; the goroutine should exit
}
