package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestRunTicker_WedgedRunIsCanceledAndTickerRecovers is the regression for
// #1017: the ticker loop calls each job synchronously, so a run blocked on a
// dependency call that never returns used to wedge that job forever. Each run
// must now be bounded by a per-invocation budget: the wedged run is canceled,
// counted as a failure, and the next tick fires and succeeds.
func TestRunTicker_WedgedRunIsCanceledAndTickerRecovers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls, successes atomic.Int32
	wedgedErr := make(chan error, 1)
	a := &App{}
	a.runTicker(ctx, "wedged", 40*time.Millisecond, func(ctx context.Context) error {
		if calls.Add(1) == 1 {
			// Models a hung DB call: it returns only once its context is done.
			<-ctx.Done()
			wedgedErr <- context.Cause(ctx)
			return ctx.Err()
		}
		successes.Add(1)
		return nil
	})

	waitForAtLeast(t, &successes, 1)

	if cause := <-wedgedErr; !errors.Is(cause, errJobRunBudgetExceeded) {
		t.Fatalf("wedged run canceled with cause %v, want errJobRunBudgetExceeded", cause)
	}
	h := findJobHealth(t, a.JobHealth(), "wedged")
	if h.Failures < 1 || h.LastFailure.IsZero() {
		t.Fatalf("wedged run not recorded as a failure: %+v", h)
	}
	if h.LastSuccess.IsZero() {
		t.Fatalf("ticker did not recover after the wedged run: %+v", h)
	}
}

// TestJobRunBudget_IsFractionOfInterval pins the budget below the interval so a
// canceled run always frees the loop before the next tick is due.
func TestJobRunBudget_IsFractionOfInterval(t *testing.T) {
	for _, interval := range []time.Duration{10 * time.Minute, 6 * time.Hour, 24 * time.Hour} {
		if got := jobRunBudget(interval); got <= 0 || got >= interval {
			t.Errorf("jobRunBudget(%v) = %v, want within (0, interval)", interval, got)
		}
	}
}
