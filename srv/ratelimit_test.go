package srv

import (
	"testing"
	"time"
)

func TestRateLimiter_AllowFirst(t *testing.T) {
	rl := newRateLimiter()
	if !rl.Allow(1, 5*time.Second) {
		t.Error("first call should be allowed")
	}
}

func TestRateLimiter_DenySecondTooSoon(t *testing.T) {
	rl := newRateLimiter()
	rl.Allow(1, 5*time.Second)
	if rl.Allow(1, 5*time.Second) {
		t.Error("second call within interval should be denied")
	}
}

func TestRateLimiter_AllowAfterInterval(t *testing.T) {
	rl := newRateLimiter()
	rl.Allow(1, 10*time.Millisecond)
	time.Sleep(15 * time.Millisecond)
	if !rl.Allow(1, 10*time.Millisecond) {
		t.Error("call after interval should be allowed")
	}
}

func TestRateLimiter_IndependentKeys(t *testing.T) {
	rl := newRateLimiter()
	rl.Allow(1, 5*time.Second)
	if !rl.Allow(2, 5*time.Second) {
		t.Error("different key should be allowed")
	}
}
