package providers

import (
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
