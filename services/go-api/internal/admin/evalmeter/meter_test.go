package evalmeter

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// The state values are the admin client's wire vocabulary, so typing State must
// leave them on the strings the client already branches on.
func TestStatus_MarshalsEachStateAsItsWireString(t *testing.T) {
	wire := map[State]string{
		StateDisabled:   `"state":"disabled"`,
		StateNoData:     `"state":"no_data"`,
		StateOK:         `"state":"ok"`,
		StateRegression: `"state":"regression"`,
		StateError:      `"state":"error"`,
	}

	for state, want := range wire {
		got, err := json.Marshal(Status{State: state})
		if err != nil {
			t.Fatalf("marshal %v: %v", state, err)
		}
		if !strings.Contains(string(got), want) {
			t.Errorf("status json = %s, want %s in it", got, want)
		}
	}
}

func TestMeter_DisabledState(t *testing.T) {
	m := New(false, 0, nil)
	if st := m.Status(); st.State != StateDisabled || st.Enabled {
		t.Fatalf("status = %+v, want disabled/!enabled", st)
	}
}

func TestMeter_NoDataBeforeFirstRun(t *testing.T) {
	m := New(true, 0, func(context.Context) (Result, error) {
		return Result{}, nil
	})
	if st := m.Status(); st.State != StateNoData {
		t.Fatalf("state = %q, want no_data before any run", st.State)
	}
}

func TestMeter_OkAndRegression(t *testing.T) {
	m := New(true, 0, func(context.Context) (Result, error) {
		return Result{Score: 0.81, Baseline: 0.80, Regressed: false}, nil
	})
	m.runOnce(context.Background())
	st := m.Status()
	if st.State != StateOK || st.Score == nil || *st.Score != 0.81 {
		t.Fatalf("status = %+v, want ok with score 0.81", st)
	}

	m2 := New(true, 0, func(context.Context) (Result, error) {
		return Result{Score: 0.70, Baseline: 0.80, Regressed: true}, nil
	})
	m2.runOnce(context.Background())
	if st := m2.Status(); st.State != StateRegression {
		t.Fatalf("state = %q, want regression", st.State)
	}
}

func TestMeter_ErrorState(t *testing.T) {
	m := New(true, 0, func(context.Context) (Result, error) {
		return Result{}, errors.New("provider unreachable")
	})
	m.runOnce(context.Background())
	if st := m.Status(); st.State != StateError || st.Error == "" {
		t.Fatalf("status = %+v, want error state", st)
	}
}

// A run whose queries errored reaches the status as a count, so an operator
// reading a depressed score can tell an outage from a ranking regression. The
// count was previously dropped at the wiring boundary.
func TestMeter_StatusSurfacesErroredQueryCount(t *testing.T) {
	m := New(true, 0, func(context.Context) (Result, error) {
		return Result{Score: 0.60, Baseline: 0.80, Regressed: false, Errored: 2}, nil
	})
	m.runOnce(context.Background())

	st := m.Status()
	if st.Errored != 2 {
		t.Errorf("Errored = %d, want 2", st.Errored)
	}
	if st.State == StateRegression {
		t.Error("a sub-baseline score with errored queries must not read as a regression")
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

// TestMeter_PanickingRunnerSurfacesAsError reproduces the crash: a runner that
// panics used to take the whole process down, since nothing between the eval
// and the scheduler goroutine recovered it.
func TestMeter_PanickingRunnerSurfacesAsError(t *testing.T) {
	m := New(true, 0, func(context.Context) (Result, error) {
		panic("scorer dereferenced a nil provider")
	})

	m.runOnce(context.Background())

	st := m.Status()
	if st.State != StateError {
		t.Fatalf("state = %q, want error after a panicking runner", st.State)
	}
	if !strings.Contains(st.Error, "scorer dereferenced a nil provider") {
		t.Errorf("error = %q, want the panic value in it", st.Error)
	}
}

// TestMeter_RunAfterAPanicExecutes covers the slot leak behind the panic: the
// run slot was cleared only on the success path, so a run that did not reach
// the end left the meter permanently "running" and skipped every later run.
func TestMeter_RunAfterAPanicExecutes(t *testing.T) {
	calls := 0
	m := New(true, 0, func(context.Context) (Result, error) {
		calls++
		if calls == 1 {
			panic("scorer dereferenced a nil provider")
		}
		return Result{Score: 0.9, Baseline: 0.8}, nil
	})

	m.runOnce(context.Background())
	m.runOnce(context.Background())

	if calls != 2 {
		t.Fatalf("runner calls = %d, want 2 (the run slot was not released)", calls)
	}
	if st := m.Status(); st.State != StateOK {
		t.Fatalf("state = %q, want ok once a later run succeeds", st.State)
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
