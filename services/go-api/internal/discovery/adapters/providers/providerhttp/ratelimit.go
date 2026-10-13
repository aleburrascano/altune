package providerhttp

import (
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const ProviderQueueDepth = 3

var errQueueTimeout = fmt.Errorf("%w: %w", ports.ErrProviderRateLimitQueueTimeout, context.DeadlineExceeded)

var errQueueFull = fmt.Errorf("%w (queue full): %w", ports.ErrProviderRateLimitQueueTimeout, context.DeadlineExceeded)

type MinIntervalLimiter struct {
	interval  time.Duration
	tolerance time.Duration
	maxWait   time.Duration
	Mu        sync.Mutex
	LastReq   time.Time
}

func NewMinIntervalLimiter(interval time.Duration) *MinIntervalLimiter {
	return NewRateLimiter(interval, 1, ProviderQueueDepth)
}

func NewRateLimiter(interval time.Duration, burst, maxQueued int) *MinIntervalLimiter {
	return &MinIntervalLimiter{
		interval:  interval,
		tolerance: time.Duration(max(burst, 1)-1) * interval,
		maxWait:   time.Duration(max(maxQueued, 0)) * interval,
	}
}

func (l *MinIntervalLimiter) Wait(ctx context.Context) error {
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

func (l *MinIntervalLimiter) nextSlot(now time.Time) (slot, start time.Time) {
	slot = l.LastReq.Add(l.interval)
	if slot.Before(now) {
		slot = now
	}
	start = slot.Add(-l.tolerance)
	if start.Before(now) {
		start = now
	}
	return slot, start
}

func (l *MinIntervalLimiter) reserve(ctx context.Context) (time.Time, error) {
	l.Mu.Lock()
	defer l.Mu.Unlock()
	now := time.Now()
	slot, start := l.nextSlot(now)
	if deadline, ok := ctx.Deadline(); ok && start.After(deadline) {
		if !now.Before(deadline) {
			return time.Time{}, context.DeadlineExceeded
		}
		return time.Time{}, errQueueTimeout
	}
	if start.Sub(now) > l.maxWait {
		return time.Time{}, errQueueFull
	}
	l.LastReq = slot
	return start, nil
}

func queueWaitErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errQueueTimeout
	}
	return err
}
