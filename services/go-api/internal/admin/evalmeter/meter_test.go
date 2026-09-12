package evalmeter

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMeter_DisabledState(t *testing.T) {
	m := New(false, 0, nil)
	if st := m.Status(); st.State != "disabled" || st.Enabled {
		t.Fatalf("status = %+v, want disabled/!enabled", st)
	}
}

func TestMeter_NoDataBeforeFirstRun(t *testing.T) {
	m := New(true, 0, func(context.Context) (Result, error) {
		return Result{}, nil
	})
	if st := m.Status(); st.State != "no_data" {
		t.Fatalf("state = %q, want no_data before any run", st.State)
	}
}

func TestMeter_OkAndRegression(t *testing.T) {
	m := New(true, 0, func(context.Context) (Result, error) {
		return Result{Score: 0.81, Baseline: 0.80, Regressed: false}, nil
	})
	m.runOnce(context.Background())
	st := m.Status()
	if st.State != "ok" || st.Score == nil || *st.Score != 0.81 {
		t.Fatalf("status = %+v, want ok with score 0.81", st)
	}

	m2 := New(true, 0, func(context.Context) (Result, error) {
		return Result{Score: 0.70, Baseline: 0.80, Regressed: true}, nil
	})
	m2.runOnce(context.Background())
	if st := m2.Status(); st.State != "regression" {
		t.Fatalf("state = %q, want regression", st.State)
	}
}

func TestMeter_ErrorState(t *testing.T) {
	m := New(true, 0, func(context.Context) (Result, error) {
		return Result{}, errors.New("provider unreachable")
	})
	m.runOnce(context.Background())
	if st := m.Status(); st.State != "error" || st.Error == "" {
		t.Fatalf("status = %+v, want error state", st)
	}
}

// TestMeter_RunnerTimeoutSurfacesAsFailure checks that a runner which only
// returns once its context is cancelled (a hung dependency) cannot stall the
// scheduler forever: runOnce returns under the bound and surfaces an error.
func TestMeter_RunnerTimeoutSurfacesAsFailure(t *testing.T) {
	m := New(true, 0, func(ctx context.Context) (Result, error) {
		<-ctx.Done()
		return Result{}, ctx.Err()
	})
	m.runTimeout = 20 * time.Millisecond

	done := make(chan struct{})
	go func() {
		m.runOnce(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runOnce hung on a blocking runner; the scheduler is stalled")
	}
	if st := m.Status(); st.State != StateError {
		t.Fatalf("state = %q, want error after a runner timeout", st.State)
	}
}

// TestMeter_PausedTickSkipsRun reproduces the runtime kill-switch gap: a paused
// meter must skip its scheduled run without a restart, and run again once
// resumed.
func TestMeter_PausedTickSkipsRun(t *testing.T) {
	calls := 0
	m := New(true, 0, func(context.Context) (Result, error) {
		calls++
		return Result{Score: 0.9, Baseline: 0.8}, nil
	})

	m.Pause()
	m.tick(context.Background())
	if calls != 0 {
		t.Fatalf("runner calls = %d, want 0 while paused", calls)
	}

	m.Resume()
	m.tick(context.Background())
	if calls != 1 {
		t.Fatalf("runner calls = %d, want 1 after resume", calls)
	}
}

func TestMeter_SkipIfRunning(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	calls := 0
	m := New(true, 0, func(context.Context) (Result, error) {
		calls++
		close(started)
		<-release
		return Result{}, nil
	})

	go m.runOnce(context.Background())
	<-started
	m.runOnce(context.Background())
	close(release)

	if calls != 1 {
		t.Fatalf("runner calls = %d, want 1 (second skipped)", calls)
	}
}
