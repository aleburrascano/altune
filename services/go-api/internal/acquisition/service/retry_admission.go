package service

import (
	"sync"
	"time"

	"altune/go-api/internal/catalog/domain"
)

// RetryCooldown is the minimum interval between manual re-acquisition retries
// for a single track.
const RetryCooldown = 60 * time.Second

// RetryAdmission rate-limits manual re-acquisition retries to at most one per
// track per cooldown window. It holds the admission state the retry endpoint
// previously kept inline in its HTTP handler — a second "should this run now?"
// guard that belongs beside the scheduler's inflight dedup, not in an adapter.
type RetryAdmission struct {
	mu        sync.Mutex
	cooldown  time.Duration
	lastRetry map[string]time.Time
}

func NewRetryAdmission() *RetryAdmission {
	return &RetryAdmission{
		cooldown:  RetryCooldown,
		lastRetry: make(map[string]time.Time),
	}
}

// Allow reports whether a retry for trackId may proceed now. When it returns
// true it records the admission; while the cooldown window is open it returns
// false. Stale entries are pruned opportunistically on each call.
func (a *RetryAdmission) Allow(trackId domain.TrackId) bool {
	key := trackId.String()

	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	if last, ok := a.lastRetry[key]; ok && now.Sub(last) < a.cooldown {
		return false
	}
	a.lastRetry[key] = now
	for k, v := range a.lastRetry {
		if now.Sub(v) >= 2*a.cooldown {
			delete(a.lastRetry, k)
		}
	}
	return true
}
