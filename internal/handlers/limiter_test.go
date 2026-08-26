package handlers

import (
	"testing"
	"time"
)

func TestUserLimiterAllowsBurst(t *testing.T) {
	l := newUserLimiter(5, 20)
	for i := 0; i < 20; i++ {
		if !l.allow(1) {
			t.Fatalf("allow #%d: expected allowed within burst", i)
		}
	}
	if l.allow(1) {
		t.Fatal("expected denial after burst exhausted")
	}
}

func TestUserLimiterRefills(t *testing.T) {
	l := newUserLimiter(10, 1)
	if !l.allow(7) {
		t.Fatal("first message should be allowed")
	}
	if l.allow(7) {
		t.Fatal("second message should be denied (burst = 1)")
	}
	// At 10 tokens/s one token refills in ~100ms; wait with margin.
	time.Sleep(150 * time.Millisecond)
	if !l.allow(7) {
		t.Fatal("expected refilled token to be allowed")
	}
}

func TestUserLimiterIsPerUser(t *testing.T) {
	l := newUserLimiter(0.001, 1)
	if !l.allow(1) {
		t.Fatal("user 1: first message should be allowed")
	}
	if l.allow(1) {
		t.Fatal("user 1: second message should be denied")
	}
	if !l.allow(2) {
		t.Fatal("user 2 must have an independent bucket")
	}
}

func TestUserLimiterSweep(t *testing.T) {
	l := newUserLimiter(5, 10)
	l.allow(1)
	l.allow(2)

	// Backdate both buckets beyond the idle timeout.
	now := time.Now()
	l.mu.Lock()
	l.buckets[1].last = now.Add(-2 * rateLimitIdleTimeout)
	l.buckets[2].last = now.Add(-2 * rateLimitIdleTimeout)
	l.mu.Unlock()

	l.sweepLocked(time.Now())
	if len(l.buckets) != 0 {
		t.Fatalf("expected all idle buckets removed, got %d", len(l.buckets))
	}
}

func TestShouldNotifyThrottles(t *testing.T) {
	l := newUserLimiter(0.001, 1)
	if !l.shouldNotify(9) {
		t.Fatal("first notice should be allowed")
	}
	if l.shouldNotify(9) {
		t.Fatal("second notice should be throttled")
	}
}
