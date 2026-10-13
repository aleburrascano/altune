package providerhttp

import (
	"altune/go-api/internal/discovery/adapters/providers/providertest"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestMinIntervalLimiterCancelledCtxReturnsPromptly(t *testing.T) {
	l := NewMinIntervalLimiter(time.Hour)

	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first wait: unexpected error %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := l.Wait(ctx)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("wait blocked for %v; should return promptly on cancellation", elapsed)
	}
}

func TestMinIntervalLimiterShortDeadlineReturnsBeforeInterval(t *testing.T) {
	l := NewMinIntervalLimiter(time.Hour)

	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first wait: unexpected error %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := l.Wait(ctx)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want context.DeadlineExceeded, got %v", err)
	}
	if elapsed >= time.Second {
		t.Fatalf("wait blocked for %v; should return before the full interval", elapsed)
	}
}

func TestMinIntervalLimiterEnforcesInterval(t *testing.T) {
	l := NewMinIntervalLimiter(50 * time.Millisecond)

	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := l.Wait(context.Background()); err != nil {
			t.Fatalf("wait %d: unexpected error %v", i, err)
		}
	}
	elapsed := time.Since(start)

	if elapsed < 100*time.Millisecond {
		t.Fatalf("three waits took %v; expected at least two full intervals", elapsed)
	}
}

func TestMinIntervalLimiterShedsWithoutReservingSlot(t *testing.T) {
	l := NewMinIntervalLimiter(time.Hour)
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first wait: unexpected error %v", err)
	}
	l.Mu.Lock()
	before := l.LastReq
	l.Mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err := l.Wait(ctx)

	if !errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want a queue timeout that is also DeadlineExceeded, got %v", err)
	}
	l.Mu.Lock()
	defer l.Mu.Unlock()
	if !l.LastReq.Equal(before) {
		t.Errorf("shed caller reserved a slot: lastReq moved from %v to %v", before, l.LastReq)
	}
}

func TestRateLimiterAdmittedCallersStaySpaced(t *testing.T) {
	const interval = 60 * time.Millisecond
	l := NewRateLimiter(interval, 1, 3)

	got := providertest.FireBurst(t, l.Wait, 10, 2*time.Second)

	var admits []time.Duration
	for _, r := range got {
		if r.Err == nil {
			admits = append(admits, r.Elapsed)
		}
	}
	if len(admits) != 4 {
		t.Fatalf("admitted %d callers, want 1 immediate + 3 queued", len(admits))
	}
	slices.Sort(admits)
	for i := 1; i < len(admits); i++ {
		if gap := admits[i] - admits[i-1]; gap < interval-10*time.Millisecond {
			t.Errorf("admits %d and %d only %v apart, want >= %v", i-1, i, gap, interval)
		}
	}
}

func TestRateLimiterQueueDrainsAndReadmits(t *testing.T) {
	const interval = 30 * time.Millisecond
	l := NewRateLimiter(interval, 1, 1)
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("second (queued): %v", err)
	}
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("third after the queue drained: %v", err)
	}
}

func TestRateLimiterShedsNoDeadlineCallerWhenFull(t *testing.T) {
	l := NewRateLimiter(time.Hour, 1, 2)
	_ = l.Wait(context.Background())
	l.Mu.Lock()
	l.LastReq = l.LastReq.Add(2 * time.Hour)
	before := l.LastReq
	l.Mu.Unlock()

	start := time.Now()
	err := l.Wait(context.Background())

	if !errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want a queue-timeout shed that is also DeadlineExceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("shed took %v, want immediate", elapsed)
	}
	l.Mu.Lock()
	defer l.Mu.Unlock()
	if !l.LastReq.Equal(before) {
		t.Errorf("shed caller reserved a slot: lastReq moved from %v to %v", before, l.LastReq)
	}
}
