package background_test

import (
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/clock/clocktest"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestRunBudget(t *testing.T) {
	if got := background.RunBudget(0); got != 0 {
		t.Errorf("RunBudget(0) = %v, want 0", got)
	}
	if got := background.RunBudget(10 * time.Minute); got != 5*time.Minute {
		t.Errorf("RunBudget(10m) = %v, want 5m", got)
	}
	for _, interval := range []time.Duration{10 * time.Minute, 6 * time.Hour, 24 * time.Hour} {
		if got := background.RunBudget(interval); got <= 0 || got >= interval {
			t.Errorf("RunBudget(%v) = %v, want within (0, interval)", interval, got)
		}
	}
}

func TestEvery_PanickingRunIsOneFailureAndLoopKeepsTicking(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var bg background.Tracker
		var jobs background.JobRegistry
		jc := jobs.Control("explode")
		var calls atomic.Int32
		clk := clocktest.NewManual(time.Now())

		err := background.Every(ctx, &bg, time.Millisecond, clk.Ticker(), func() {
			background.RunJob(ctx, jc, "explode", time.Second, func(context.Context) error {
				if calls.Add(1) == 1 {
					panic("boom")
				}
				return nil
			})
		})
		synctest.Wait()
		clk.Advance(time.Millisecond)
		synctest.Wait()
		cancel()
		bg.Wait()

		if err != nil {
			t.Fatalf("Every = %v, want nil", err)
		}
		if got := healthOf(t, &jobs, "explode").Failures; got != 1 {
			t.Errorf("failures = %d, want 1", got)
		}
		if calls.Load() != 2 {
			t.Errorf("loop ran %d times, want 2: once at start, once after the panic on the next tick", calls.Load())
		}
	})
}

func TestEvery_RefusesAMissingTicker(t *testing.T) {
	var bg background.Tracker
	var ran atomic.Bool

	err := background.Every(context.Background(), &bg, time.Second, nil, func() { ran.Store(true) })
	bg.Wait()

	if !errors.Is(err, background.ErrTickerRequired) || !strings.Contains(err.Error(), "background.Every") {
		t.Errorf("Every(nil ticker) = %v, want ErrTickerRequired naming Every", err)
	}
	if ran.Load() {
		t.Error("the job ran without a ticker")
	}
}

func TestEvery_RunsAtAFixedRateEvenWhenARunIsSlow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var bg background.Tracker
		clk := clocktest.NewManual(time.Now())
		var starts []time.Time

		err := background.Every(ctx, &bg, 10*time.Second, clk.Ticker(), func() {
			starts = append(starts, clk.Now())
			if len(starts) == 2 {
				clk.Advance(4 * time.Second)
			}
		})
		synctest.Wait()
		clk.Advance(10 * time.Second)
		synctest.Wait()
		clk.Advance(6 * time.Second)
		synctest.Wait()
		cancel()
		bg.Wait()

		if err != nil {
			t.Fatalf("Every = %v, want nil", err)
		}
		if len(starts) != 3 {
			t.Fatalf("runs = %d, want 3 (start, 10s, 20s)", len(starts))
		}
		if got := starts[2].Sub(starts[0]); got != 20*time.Second {
			t.Errorf("third run %v after the first, want 20s: a slow run must not push the schedule", got)
		}
	})
}

func TestRunJob_OverBudgetIsCanceledAndCounted(t *testing.T) {
	var jc background.JobControl
	var cause error
	background.RunJob(context.Background(), &jc, "wedged", 10*time.Millisecond, func(ctx context.Context) error {
		<-ctx.Done()
		cause = context.Cause(ctx)
		return ctx.Err()
	})

	if !errors.Is(cause, background.ErrRunBudgetExceeded) {
		t.Errorf("run canceled with cause %v, want ErrRunBudgetExceeded", cause)
	}
	if got := jc.ConsecutiveFailures(); got != 1 {
		t.Errorf("ConsecutiveFailures = %d, want 1", got)
	}
}

func TestRunJob_ParentCanceledIsNotAFailure(t *testing.T) {
	var jobs background.JobRegistry
	jc := jobs.Control("cut")
	parent, cancel := context.WithCancel(context.Background())
	background.RunJob(parent, jc, "cut", time.Minute, func(ctx context.Context) error {
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
	var bg background.Tracker
	var jobs background.JobRegistry
	jc := jobs.Control("j")
	var runs, ticks atomic.Int32
	firstDone := make(chan struct{})
	secondTick := make(chan struct{})

	background.Every(ctx, &bg, time.Millisecond, clock.NewTicker, func() {
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
	bg.Wait()

	if got := runs.Load(); got != 1 {
		t.Errorf("runs = %d, want 1: later ticks must be skipped once disabled", got)
	}
	if got := healthOf(t, &jobs, "j").Skipped; got < 1 {
		t.Errorf("Skipped = %d, want at least 1", got)
	}
}

func TestStartWhenLeader_AwaitFalseStartsNothing(t *testing.T) {
	var bg background.Tracker
	var started atomic.Int32
	background.StartWhenLeader(context.Background(), &bg, func(context.Context) bool { return false }, []background.NamedStart{
		{Name: "a", Start: func(context.Context) { started.Add(1) }},
		{Name: "b", Start: func(context.Context) { started.Add(1) }},
	})
	bg.Wait()

	if got := started.Load(); got != 0 {
		t.Errorf("started %d jobs, want 0", got)
	}
}

func TestStartWhenLeader_PanicInOneStartDoesNotStopTheNext(t *testing.T) {
	var bg background.Tracker
	var second atomic.Bool
	background.StartWhenLeader(context.Background(), &bg, func(context.Context) bool { return true }, []background.NamedStart{
		{Name: "panicky", Start: func(context.Context) { panic("boom") }},
		{Name: "survivor", Start: func(context.Context) { second.Store(true) }},
	})
	bg.Wait()

	if !second.Load() {
		t.Error("a panic in one start prevented the next from running")
	}
}

func healthOf(t *testing.T, jobs *background.JobRegistry, name string) background.JobHealth {
	t.Helper()
	for _, h := range jobs.Health() {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("no health reported for job %q", name)
	return background.JobHealth{}
}

func TestRunJob_PanicIsOneFailureLoggedWithTheJobAndItsContext(t *testing.T) {
	ring := captureDetachedLogs(t)
	var jc background.JobControl
	ctx := logging.ContextWithCorrelationID(context.Background(), "corr-job-5")

	background.RunJob(ctx, &jc, "explode", time.Second, func(context.Context) error { panic("boom") })

	if got := jc.ConsecutiveFailures(); got != 1 {
		t.Errorf("ConsecutiveFailures = %d, want 1", got)
	}
	rec := onlyRecord(t, ring, "background_job.panic")
	if rec.Attrs["job"] != "explode" || rec.Attrs["corr_id"] != "corr-job-5" {
		t.Errorf("record attrs job=%q corr_id=%q, want the job name and the caller's corr_id", rec.Attrs["job"], rec.Attrs["corr_id"])
	}
}

func TestRunJob_EveryLogInARunCarriesTheJobAndAFreshRunID(t *testing.T) {
	ring := captureLogsAt(t, "info")
	var jc background.JobControl
	run := func(msg string) {
		background.RunJob(context.Background(), &jc, "reconcile", time.Second, func(ctx context.Context) error {
			slog.InfoContext(ctx, msg)
			return nil
		})
	}

	run("first run line")
	run("second run line")

	first, second := onlyRecord(t, ring, "first run line"), onlyRecord(t, ring, "second run line")
	for _, rec := range []logging.CapturedRecord{first, second} {
		if rec.Attrs["job"] != "reconcile" || rec.Attrs["run_id"] == "" {
			t.Errorf("record %q attrs job=%q run_id=%q, want the job name and a run_id", rec.Message, rec.Attrs["job"], rec.Attrs["run_id"])
		}
	}
	if first.Attrs["run_id"] == second.Attrs["run_id"] {
		t.Errorf("both runs logged run_id %q, want a fresh id per run", first.Attrs["run_id"])
	}
}

func TestRunJob_TheBudgetWarningCarriesTheSameRunIDAsTheRunsOwnLogs(t *testing.T) {
	ring := captureLogsAt(t, "info")
	var jc background.JobControl

	background.RunJob(context.Background(), &jc, "wedged", 10*time.Millisecond, func(ctx context.Context) error {
		slog.InfoContext(ctx, "run started")
		<-ctx.Done()
		return ctx.Err()
	})

	started := onlyRecord(t, ring, "run started")
	warned := onlyRecord(t, ring, "background job run exceeded its budget; canceled")
	if started.Attrs["run_id"] == "" || started.Attrs["run_id"] != warned.Attrs["run_id"] {
		t.Errorf("run_id on the run's log %q and on the budget warning %q, want the same non-empty id", started.Attrs["run_id"], warned.Attrs["run_id"])
	}
}
