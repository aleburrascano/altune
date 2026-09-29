package providers

import (
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const providerQueueDepth = 3

var errQueueTimeout = fmt.Errorf("%w: %w", ports.ErrProviderRateLimitQueueTimeout, context.DeadlineExceeded)

var errQueueFull = fmt.Errorf("%w (queue full): %w", ports.ErrProviderRateLimitQueueTimeout, context.DeadlineExceeded)

type minIntervalLimiter struct {
	interval  time.Duration
	tolerance time.Duration
	maxWait   time.Duration
	mu        sync.Mutex
	lastReq   time.Time
}

func newMinIntervalLimiter(interval time.Duration) *minIntervalLimiter {
	return newRateLimiter(interval, 1, providerQueueDepth)
}

func newRateLimiter(interval time.Duration, burst, maxQueued int) *minIntervalLimiter {
	return &minIntervalLimiter{
		interval:  interval,
		tolerance: time.Duration(max(burst, 1)-1) * interval,
		maxWait:   time.Duration(max(maxQueued, 0)) * interval,
	}
}

func (l *minIntervalLimiter) wait(ctx context.Context) error {
	start, err := l.reserve(ctx)
	if err != nil {
		return err
	}
	wait := time.Until(start)
	if wait <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return queueWaitErr(ctx.Err())
	}
}

func (l *minIntervalLimiter) reserve(ctx context.Context) (time.Time, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	slot := l.lastReq.Add(l.interval)
	if slot.Before(now) {
		slot = now
	}
	start := slot.Add(-l.tolerance)
	if start.Before(now) {
		start = now
	}
	if deadline, ok := ctx.Deadline(); ok && start.After(deadline) {
		if !now.Before(deadline) {
			return time.Time{}, context.DeadlineExceeded
		}
		return time.Time{}, errQueueTimeout
	}
	if start.Sub(now) > l.maxWait {
		return time.Time{}, errQueueFull
	}
	l.lastReq = slot
	return start, nil
}

func queueWaitErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errQueueTimeout
	}
	return err
}
