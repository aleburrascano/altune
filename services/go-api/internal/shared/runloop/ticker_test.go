package runloop_test

import (
	"altune/go-api/internal/shared/runloop"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunBudget(t *testing.T) {
	if got := runloop.RunBudget(0); got != 0 {
		t.Errorf("RunBudget(0) = %v, want 0", got)
	}
	if got := runloop.RunBudget(10 * time.Minute); got != 5*time.Minute {
		t.Errorf("RunBudget(10m) = %v, want 5m", got)
	}
	for _, interval := range []time.Duration{10 * time.Minute, 6 * time.Hour, 24 * time.Hour} {
		if got := runloop.RunBudget(interval); got <= 0 || got >= interval {
			t.Errorf("RunBudget(%v) = %v, want within (0, interval)", interval, got)
		}
	}
}

func TestEvery_PanickingRunIsOneFailureAndLoopKeepsTicking(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	var jobs runloop.JobRegistry
	jc := jobs.Control("explode")
	var calls atomic.Int32
	ran := make(chan struct{}, 8)

	runloop.Every(ctx, &wg, time.Millisecond, func() {
		runloop.RunJob(ctx, jc, "explode", time.Second, func(context.Context) error {
			n := calls.Add(1)
			ran <- struct{}{}
			if n == 1 {
				panic("boom")
			}
			return nil
		})
	})

	<-ran
	<-ran
	cancel()
	wg.Wait()

	if got := healthOf(t, &jobs, "explode").Failures; got != 1 {
		t.Errorf("failures = %d, want 1", got)
	}
	if calls.Load() < 2 {
		t.Errorf("loop ran %d times, want it to keep ticking after the panic", calls.Load())
	}
}

func TestRunJob_OverBudgetIsCanceledAndCounted(t *testing.T) {
	var jc runloop.JobControl
	var cause error
	runloop.RunJob(context.Background(), &jc, "wedged", 10*time.Millisecond, func(ctx context.Context) error {
		<-ctx.Done()
		cause = context.Cause(ctx)
		return ctx.Err()
	})

	if !errors.Is(cause, runloop.ErrRunBudgetExceeded) {
		t.Errorf("run canceled with cause %v, want ErrRunBudgetExceeded", cause)
	}
	if got := jc.ConsecutiveFailures(); got != 1 {
		t.Errorf("ConsecutiveFailures = %d, want 1", got)
	}
}

func TestRunJob_ParentCanceledIsNotAFailure(t *testing.T) {
	var jobs runloop.JobRegistry
	jc := jobs.Control("cut")
	parent, cancel := context.WithCancel(context.Background())
	runloop.RunJob(parent, jc, "cut", time.Minute, func(ctx context.Context) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})

	if got := jc.ConsecutiveFailures(); got != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0 for a run cut short by its parent", got)
	}
	if got := healthOf(t, &jobs, "cut").Failures; got != 0 {
		t.Errorf("Failures = %d, want 0", got)
	}
}

func TestEvery_JobDisabledBetweenTicksIsSkipped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	var jobs runloop.JobRegistry
	jc := jobs.Control("j")
	var runs, ticks atomic.Int32
	firstDone := make(chan struct{})
	secondTick := make(chan struct{})

	runloop.Every(ctx, &wg, time.Millisecond, func() {
		n := ticks.Add(1)
		if jc.Admit() {
			runs.Add(1)
			jc.DisableIfEnabled()
			close(firstDone)
		}
		if n == 3 {
			close(secondTick)
		}
	})

	<-firstDone
	<-secondTick
	cancel()
	wg.Wait()

	if got := runs.Load(); got != 1 {
		t.Errorf("runs = %d, want 1: later ticks must be skipped once disabled", got)
	}
	if got := healthOf(t, &jobs, "j").Skipped; got < 1 {
		t.Errorf("Skipped = %d, want at least 1", got)
	}
}

func TestStartWhenLeader_AwaitFalseStartsNothing(t *testing.T) {
	var wg sync.WaitGroup
	var started atomic.Int32
	runloop.StartWhenLeader(context.Background(), &wg, func(context.Context) bool { return false }, []runloop.NamedStart{
		{Name: "a", Start: func(context.Context) { started.Add(1) }},
		{Name: "b", Start: func(context.Context) { started.Add(1) }},
	})
	wg.Wait()

	if got := started.Load(); got != 0 {
		t.Errorf("started %d jobs, want 0", got)
	}
}

func TestStartWhenLeader_PanicInOneStartDoesNotStopTheNext(t *testing.T) {
	var wg sync.WaitGroup
	var second atomic.Bool
	runloop.StartWhenLeader(context.Background(), &wg, func(context.Context) bool { return true }, []runloop.NamedStart{
		{Name: "panicky", Start: func(context.Context) { panic("boom") }},
		{Name: "survivor", Start: func(context.Context) { second.Store(true) }},
	})
	wg.Wait()

	if !second.Load() {
		t.Error("a panic in one start prevented the next from running")
	}
}

func healthOf(t *testing.T, jobs *runloop.JobRegistry, name string) runloop.JobHealth {
	t.Helper()
	for _, h := range jobs.Health() {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("no health reported for job %q", name)
	return runloop.JobHealth{}
}
