package providers

import (
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// providerQueueDepth caps how many callers a provider limiter parks for a later
// slot. Each adapter's limiter is shared by every concurrent request, so an
// uncapped queue makes the Nth caller wait N intervals: load past the upstream
// rate piles up as parked goroutines instead of being turned away. Three 1s
// slots keeps the longest wait well inside the 4-5s search budgets.
const providerQueueDepth = 3

// errQueueTimeout is returned when a call gives up waiting for its slot. It
// matches ports.ErrProviderRateLimitQueueTimeout, so the circuit breaker does
// not count it, and context.DeadlineExceeded, so callers that only look for a
// timeout still see one.
var errQueueTimeout = fmt.Errorf("%w: %w", ports.ErrProviderRateLimitQueueTimeout, context.DeadlineExceeded)

// errQueueFull is returned to a caller shed because the queue is already
// providerQueueDepth deep. It carries the same sentinel as errQueueTimeout: the
// call never reached the provider, and its wait would have exceeded the queue's
// budget, so the breaker, search statuses and content statuses must read it
// exactly as they read a deadline shed.
var errQueueFull = fmt.Errorf("%w (queue full): %w", ports.ErrProviderRateLimitQueueTimeout, context.DeadlineExceeded)

// minIntervalLimiter spaces calls to one provider. It is a GCRA: slots are
// interval apart, a burst of up to burst calls may run ahead of their slots,
// and at most maxWait of queueing is handed out before callers are shed.
type minIntervalLimiter struct {
	interval  time.Duration
	tolerance time.Duration
	maxWait   time.Duration
	mu        sync.Mutex
	// lastReq is the theoretical time of the latest reserved slot.
	lastReq time.Time
}

// newMinIntervalLimiter spaces calls interval apart with no burst.
func newMinIntervalLimiter(interval time.Duration) *minIntervalLimiter {
	return newRateLimiter(interval, 1, providerQueueDepth)
}

// newRateLimiter admits burst calls at once, then one per interval, parking at
// most maxQueued callers for a later slot and shedding the rest.
func newRateLimiter(interval time.Duration, burst, maxQueued int) *minIntervalLimiter {
	return &minIntervalLimiter{
		interval:  interval,
		tolerance: time.Duration(max(burst, 1)-1) * interval,
		maxWait:   time.Duration(max(maxQueued, 0)) * interval,
	}
}

// wait blocks until the caller's slot comes up. A caller whose slot would land
// after its context deadline, or behind a full queue, is shed at once without
// reserving the slot, so it neither sits in the queue until the deadline fires
// nor pushes back the callers behind it.
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

// reserve claims the next free slot and returns when the caller may start.
// When that start is past ctx's deadline it claims nothing and returns
// errQueueTimeout, or plain DeadlineExceeded if the deadline had already passed
// on arrival: that budget was spent upstream (say, by an earlier call that
// hung), not in this queue. When the start is further out than the queue's
// budget it claims nothing and returns errQueueFull.
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

// queueWaitErr maps a context error that ended a queue wait: a deadline that
// fired while queued is a queue timeout; cancellation stays cancellation.
func queueWaitErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errQueueTimeout
	}
	return err
}
