package providerhttp_test

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/discovery/internal/adapters/providers/providertest"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestMinIntervalLimiterCancelledCtxReturnsPromptly(t *testing.T) {
	l := discovery.NewProviderHTTPMinIntervalLimiter(time.Hour)

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
	l := discovery.NewProviderHTTPMinIntervalLimiter(time.Hour)

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
	l := discovery.NewProviderHTTPMinIntervalLimiter(50 * time.Millisecond)

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
	l := discovery.NewProviderHTTPMinIntervalLimiter(time.Hour)
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first wait: unexpected error %v", err)
	}
	l.Mu.Lock()
	before := l.LastReq
	l.Mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err := l.Wait(ctx)

	if !errors.Is(err, discovery.ErrProviderRateLimitQueueTimeout) || !errors.Is(err, context.DeadlineExceeded) {
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
	l := discovery.NewProviderHTTPRateLimiter(interval, 1, 3)

	var mu sync.Mutex
	var starts []time.Time
	waitAndRecord := func(ctx context.Context) error {
		if err := l.Wait(ctx); err != nil {
			return err
		}
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		return nil
	}

	got := providertest.FireBurst(t, waitAndRecord, 10, 2*time.Second)

	admitted := 0
	for _, r := range got {
		if r.Err == nil {
			admitted++
		}
	}
	if admitted != 4 {
		t.Fatalf("admitted %d callers, want 1 immediate + 3 queued", admitted)
	}
	mu.Lock()
	admits := slices.Clone(starts)
	mu.Unlock()
	if len(admits) != 4 {
		t.Fatalf("limiter reserved %d slots, want 4", len(admits))
	}
	slices.SortFunc(admits, func(a, b time.Time) int { return a.Compare(b) })
	const stampSlack = 5 * time.Millisecond
	want := time.Duration(len(admits)-1) * interval
	if span := admits[len(admits)-1].Sub(admits[0]); span < want-stampSlack {
		t.Errorf("first to last admit took %v, want at least %v for %d callers spaced %v apart", span, want, len(admits), interval)
	}
	for i := 1; i < len(admits); i++ {
		if gap := admits[i].Sub(admits[i-1]); gap < interval/2 {
			t.Errorf("admits %d and %d only %v apart, want about %v", i-1, i, gap, interval)
		}
	}
}

func TestRateLimiterQueueDrainsAndReadmits(t *testing.T) {
	const interval = 30 * time.Millisecond
	l := discovery.NewProviderHTTPRateLimiter(interval, 1, 1)
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
	l := discovery.NewProviderHTTPRateLimiter(time.Hour, 1, 2)
	_ = l.Wait(context.Background())
	l.Mu.Lock()
	l.LastReq = l.LastReq.Add(2 * time.Hour)
	before := l.LastReq
	l.Mu.Unlock()

	start := time.Now()
	err := l.Wait(context.Background())

	if !errors.Is(err, discovery.ErrProviderRateLimitQueueTimeout) || !errors.Is(err, context.DeadlineExceeded) {
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
