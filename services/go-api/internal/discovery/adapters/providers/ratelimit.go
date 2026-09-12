package providers

import (
	"context"
	"sync"
	"time"
)

type minIntervalLimiter struct {
	interval time.Duration
	mu       sync.Mutex
	lastReq  time.Time
}

func newMinIntervalLimiter(interval time.Duration) *minIntervalLimiter {
	return &minIntervalLimiter{interval: interval}
}

func (l *minIntervalLimiter) wait(ctx context.Context) error {
	l.mu.Lock()
	next := l.lastReq.Add(l.interval)
	if now := time.Now(); next.Before(now) {
		next = now
	}
	l.lastReq = next
	wait := time.Until(next)
	l.mu.Unlock()

	if wait <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
