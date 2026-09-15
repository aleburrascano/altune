package providers

import (
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"
)

// burstResult is what one caller in a concurrent burst saw from the limiter.
type burstResult struct {
	err     error
	elapsed time.Duration
}

// fireBurst sends n concurrent callers at wait, each with a deadline far past
// any sane queue budget, and returns what the callers that finished within
// window saw. The rest are cancelled and drained before it returns.
func fireBurst(t *testing.T, wait func(context.Context) error, n int, window time.Duration) []burstResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	results := make(chan burstResult, n)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := wait(ctx)
			results <- burstResult{err: err, elapsed: time.Since(start)}
		}()
	}

	timer := time.NewTimer(window)
	defer timer.Stop()
	var done []burstResult
collect:
	for len(done) < n {
		select {
		case r := <-results:
			done = append(done, r)
		case <-timer.C:
			break collect
		}
	}
	cancel()
	wg.Wait()
	return done
}

// A burst of concurrent callers into a production-configured provider limiter
// admits its burst at once, parks at most providerQueueDepth callers for later
// slots, and sheds every other caller promptly with the queue-timeout sentinel
// the circuit breaker ignores, instead of parking it for as long as its
// deadline allows.
func TestProviderLimiterBurstShedsBeyondQueueDepth(t *testing.T) {
	const callers = 20
	cases := []struct {
		name  string
		wait  func(context.Context) error
		burst int
	}{
		{"musicbrainz", NewMusicBrainzAdapter(http.DefaultClient, "ua").limiter.wait, 1},
		{"discogs", NewDiscogsAdapter(http.DefaultClient, "tok", "ua").limiter.wait, 1},
		{"itunes", NewITunesAdapter(http.DefaultClient).limiter.wait, itunesBurst},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fireBurst(t, tc.wait, callers, 300*time.Millisecond)

			admitted, shed := 0, 0
			for _, r := range got {
				switch {
				case r.err == nil:
					admitted++
				case errors.Is(r.err, ports.ErrProviderRateLimitQueueTimeout):
					shed++
				default:
					t.Errorf("caller got %v, want nil or a queue-timeout shed", r.err)
				}
			}
			if admitted != tc.burst {
				t.Errorf("admitted at once = %d, want the burst of %d", admitted, tc.burst)
			}
			if want := callers - tc.burst - providerQueueDepth; shed != want {
				t.Errorf("shed within 300ms = %d, want %d (callers %d - burst %d - queue depth %d)",
					shed, want, callers, tc.burst, providerQueueDepth)
			}
		})
	}
}

// The depth bound does not loosen politeness: callers admitted out of a burst
// still get slots at least one interval apart.
func TestRateLimiterAdmittedCallersStaySpaced(t *testing.T) {
	const interval = 60 * time.Millisecond
	l := newRateLimiter(interval, 1, 3)

	got := fireBurst(t, l.wait, 10, 2*time.Second)

	var admits []time.Duration
	for _, r := range got {
		if r.err == nil {
			admits = append(admits, r.elapsed)
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

// The queue depth frees up as slots are consumed: once the queue drains, a new
// caller is admitted rather than shed.
func TestRateLimiterQueueDrainsAndReadmits(t *testing.T) {
	const interval = 30 * time.Millisecond
	l := newRateLimiter(interval, 1, 1)
	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("second (queued): %v", err)
	}
	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("third after the queue drained: %v", err)
	}
}

// A caller with no deadline at all is still shed once the queue is full.
func TestRateLimiterShedsNoDeadlineCallerWhenFull(t *testing.T) {
	l := newRateLimiter(time.Hour, 1, 2)
	_ = l.wait(context.Background())
	l.mu.Lock()
	l.lastReq = l.lastReq.Add(2 * time.Hour) // two callers already queued
	before := l.lastReq
	l.mu.Unlock()

	start := time.Now()
	err := l.wait(context.Background())

	if !errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want a queue-timeout shed that is also DeadlineExceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("shed took %v, want immediate", elapsed)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.lastReq.Equal(before) {
		t.Errorf("shed caller reserved a slot: lastReq moved from %v to %v", before, l.lastReq)
	}
}

// iTunes keeps its burst: the first itunesBurst calls go straight through.
func TestITunesLimiterAllowsBurst(t *testing.T) {
	a := NewITunesAdapter(http.DefaultClient)
	start := time.Now()
	for i := 0; i < itunesBurst; i++ {
		if err := a.limiter.wait(context.Background()); err != nil {
			t.Fatalf("burst call %d: %v", i, err)
		}
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("burst of %d took %v, want immediate", itunesBurst, elapsed)
	}
}
