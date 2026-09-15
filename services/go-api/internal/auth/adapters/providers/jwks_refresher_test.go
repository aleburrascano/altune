package providers

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// newTestRefresher returns a refresher whose fetch fails with fetchErr (nil
// for success) and counts calls, with a controllable clock and no jitter.
func newTestRefresher(fetchErr *error) (*jwksRefresher, *atomic.Int64, *time.Time) {
	var calls atomic.Int64
	clock := time.Unix(1_700_000_000, 0)
	r := newJWKSRefresher(func(context.Context) error {
		calls.Add(1)
		return *fetchErr
	})
	r.now = func() time.Time { return clock }
	r.jitter = func(d time.Duration) time.Duration { return d }
	return r, &calls, &clock
}

func TestJWKSRefresher_BackoffDoublesToCapAndResetsOnSuccess(t *testing.T) {
	fetchErr := errors.New("jwks down")
	r, calls, clock := newTestRefresher(&fetchErr)
	ctx := context.Background()

	wantDelays := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, jwksRefreshBackoffCap, jwksRefreshBackoffCap}
	for i, want := range wantDelays {
		if err := r.Refresh(ctx); !errors.Is(err, fetchErr) {
			t.Fatalf("attempt %d: want fetch error, got %v", i+1, err)
		}
		*clock = clock.Add(want - time.Millisecond)
		if err := r.Refresh(ctx); !errors.Is(err, errJWKSRefreshBackoff) || !errors.Is(err, fetchErr) {
			t.Fatalf("attempt %d: want backoff wrapping last failure just before %s, got %v", i+1, want, err)
		}
		*clock = clock.Add(time.Millisecond)
	}
	if got := calls.Load(); got != int64(len(wantDelays)) {
		t.Fatalf("fetches: got %d, want %d (one per elapsed window)", got, len(wantDelays))
	}

	fetchErr = nil
	if err := r.Refresh(ctx); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	fetchErr = errors.New("down again")
	_ = r.Refresh(ctx)
	*clock = clock.Add(time.Second)
	if err := r.Refresh(ctx); errors.Is(err, errJWKSRefreshBackoff) {
		t.Fatalf("backoff did not reset to base after a success: %v", err)
	}
}

func TestEqualJitter_StaysWithinHalfToFull(t *testing.T) {
	for range 1000 {
		if got := equalJitter(jwksRefreshBackoffCap); got < jwksRefreshBackoffCap/2 || got > jwksRefreshBackoffCap {
			t.Fatalf("equalJitter(%s) = %s, want within [%s, %s]", jwksRefreshBackoffCap, got, jwksRefreshBackoffCap/2, jwksRefreshBackoffCap)
		}
	}
}
