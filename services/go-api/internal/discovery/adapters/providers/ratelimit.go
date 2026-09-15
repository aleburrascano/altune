package providers

import (
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// errQueueTimeout is returned when a call gives up waiting for its slot. It
// matches ports.ErrProviderRateLimitQueueTimeout, so the circuit breaker does
// not count it, and context.DeadlineExceeded, so callers that only look for a
// timeout still see one.
var errQueueTimeout = fmt.Errorf("%w: %w", ports.ErrProviderRateLimitQueueTimeout, context.DeadlineExceeded)

type minIntervalLimiter struct {
	interval time.Duration
	mu       sync.Mutex
	lastReq  time.Time
}

func newMinIntervalLimiter(interval time.Duration) *minIntervalLimiter {
	return &minIntervalLimiter{interval: interval}
}

// wait blocks until the caller's slot comes up. A caller whose slot would land
// after its context deadline is shed at once, without reserving the slot, so
// it neither sits in the queue until the deadline fires nor pushes back the
// callers behind it.
func (l *minIntervalLimiter) wait(ctx context.Context) error {
	next, err := l.reserve(ctx)
	if err != nil {
		return err
	}
	wait := time.Until(next)
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

// reserve claims the next free slot. When that slot is past ctx's deadline it
// claims nothing and returns errQueueTimeout, or plain DeadlineExceeded if the
// deadline had already passed on arrival: that budget was spent upstream (say,
// by an earlier call that hung), not in this queue.
func (l *minIntervalLimiter) reserve(ctx context.Context) (time.Time, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	next := l.lastReq.Add(l.interval)
	if next.Before(now) {
		next = now
	}
	if deadline, ok := ctx.Deadline(); ok && next.After(deadline) {
		if !now.Before(deadline) {
			return time.Time{}, context.DeadlineExceeded
		}
		return time.Time{}, errQueueTimeout
	}
	l.lastReq = next
	return next, nil
}

// queueWaitErr maps a context error that ended a queue wait: a deadline that
// fired while queued is a queue timeout; cancellation stays cancellation.
func queueWaitErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errQueueTimeout
	}
	return err
}
