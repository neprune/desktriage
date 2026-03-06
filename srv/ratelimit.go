package srv

import (
	"sync"
	"time"
)

type rateLimiter struct {
	mu   sync.Mutex
	last map[int64]time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		last: make(map[int64]time.Time),
	}
}

// Allow reports whether key is allowed to proceed given minInterval.
// It returns true if key has never been seen or if at least minInterval
// has elapsed since the last allowed call. On true it records the
// current time; on false the stored timestamp is left unchanged.
func (rl *rateLimiter) Allow(key int64, minInterval time.Duration) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	if last, ok := rl.last[key]; ok && now.Sub(last) < minInterval {
		return false
	}
	rl.last[key] = now
	return true
}
