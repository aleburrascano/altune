package providers

import (
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"testing"
	"time"
)

func TestMinIntervalLimiterCancelledCtxReturnsPromptly(t *testing.T) {
	l := newMinIntervalLimiter(time.Hour)

	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("first wait: unexpected error %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := l.wait(ctx)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("wait blocked for %v; should return promptly on cancellation", elapsed)
	}
}

func TestMinIntervalLimiterShortDeadlineReturnsBeforeInterval(t *testing.T) {
	l := newMinIntervalLimiter(time.Hour)

	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("first wait: unexpected error %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := l.wait(ctx)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want context.DeadlineExceeded, got %v", err)
	}
	if elapsed >= time.Second {
		t.Fatalf("wait blocked for %v; should return before the full interval", elapsed)
	}
}

func TestMinIntervalLimiterEnforcesInterval(t *testing.T) {
	l := newMinIntervalLimiter(50 * time.Millisecond)

	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := l.wait(context.Background()); err != nil {
			t.Fatalf("wait %d: unexpected error %v", i, err)
		}
	}
	elapsed := time.Since(start)

	if elapsed < 100*time.Millisecond {
		t.Fatalf("three waits took %v; expected at least two full intervals", elapsed)
	}
}

func TestMinIntervalLimiterShedsWithoutReservingSlot(t *testing.T) {
	l := newMinIntervalLimiter(time.Hour)
	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("first wait: unexpected error %v", err)
	}
	l.mu.Lock()
	before := l.lastReq
	l.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err := l.wait(ctx)

	if !errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want a queue timeout that is also DeadlineExceeded, got %v", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.lastReq.Equal(before) {
		t.Errorf("shed caller reserved a slot: lastReq moved from %v to %v", before, l.lastReq)
	}
}

// A caller whose deadline had already passed before it reached the queue spent
// its budget elsewhere; that is a plain timeout, not a queue timeout.
func TestMinIntervalLimiterExpiredOnArrivalIsNotQueueTimeout(t *testing.T) {
	l := newMinIntervalLimiter(time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	err := l.wait(ctx)

	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) {
		t.Fatalf("want plain DeadlineExceeded, got %v", err)
	}
}
