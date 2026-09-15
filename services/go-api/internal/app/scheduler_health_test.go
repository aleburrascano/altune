package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestRunTicker_KillSwitchStopsAndResumesJob is the regression for the missing
// runtime kill switch: a background job must be toggleable at runtime without a
// redeploy. A disabled job stays registered and keeps ticking, but every tick
// returns early without doing work; re-enabling resumes it in place.
func TestRunTicker_KillSwitchStopsAndResumesJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var runs atomic.Int32
	a := &App{}
	a.runTicker(ctx, "sweep", time.Millisecond, func(context.Context) error {
		runs.Add(1)
		return nil
	})

	// Running by default.
	waitForAtLeast(t, &runs, 3)

	// Flip the kill switch and let any in-flight tick drain.
	a.SetJobEnabled("sweep", false)
	time.Sleep(30 * time.Millisecond)
	baseline := runs.Load()

	// Across many further ticks the disabled job must not do any work.
	time.Sleep(60 * time.Millisecond)
	if extra := runs.Load() - baseline; extra > 0 {
		t.Fatalf("disabled job kept running: %d extra runs", extra)
	}

	// Re-enabling resumes it without a restart.
	a.SetJobEnabled("sweep", true)
	waitForAtLeast(t, &runs, baseline+3)
}

// TestJobHealth_RecordsSuccessAndFailure is the regression for the missing
// health signal: each run must leave a queryable last-success/last-failure
// timestamp and a cumulative failure count per job.
func TestJobHealth_RecordsSuccessAndFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	a := &App{}
	// Odd calls succeed, even calls fail, so both signals accumulate.
	a.runTicker(ctx, "rollup", time.Millisecond, func(context.Context) error {
		if calls.Add(1)%2 == 0 {
			return errors.New("boom")
		}
		return nil
	})

	waitForAtLeast(t, &calls, 4)
	cancel()

	h := findJobHealth(t, a.JobHealth(), "rollup")
	if !h.Enabled {
		t.Error("job should report enabled when its kill switch is untouched")
	}
	if h.LastSuccess.IsZero() {
		t.Error("health signal did not record a last-success timestamp")
	}
	if h.Failures == 0 {
		t.Error("health signal did not count any failures")
	}
	if h.LastFailure.IsZero() {
		t.Error("health signal did not record a last-failure timestamp")
	}
}

// TestJobHealth_ReflectsKillSwitch confirms the queryable snapshot tracks the
// kill switch so an operator can see which jobs are currently suspended.
func TestJobHealth_ReflectsKillSwitch(t *testing.T) {
	a := &App{}
	a.job("paused")
	if _, ok := a.SetJobEnabled("paused", false); !ok {
		t.Fatal("SetJobEnabled on a registered job reported unknown")
	}

	h := findJobHealth(t, a.JobHealth(), "paused")
	if h.Enabled {
		t.Error("a disabled job must report Enabled=false in its health snapshot")
	}
}

func findJobHealth(t *testing.T, snapshot []JobHealth, name string) JobHealth {
	t.Helper()
	for _, h := range snapshot {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("job %q not found in health snapshot", name)
	return JobHealth{}
}

// TestSetJobEnabled_UnknownJobRegistersNothing guards the admin kill switch
// against a mistyped job name minting a phantom job in the health snapshot.
func TestSetJobEnabled_UnknownJobRegistersNothing(t *testing.T) {
	a := &App{}
	if _, ok := a.SetJobEnabled("typo", false); ok {
		t.Fatal("SetJobEnabled on an unregistered job reported ok")
	}
	if got := a.JobHealth(); len(got) != 0 {
		t.Fatalf("unknown job name registered a job: %+v", got)
	}
}

// TestStartTicker_RegistersJobBeforeLeadership confirms a job is listed (and
// its kill switch flippable) on an instance that has not acquired leadership.
func TestStartTicker_RegistersJobBeforeLeadership(t *testing.T) {
	a := &App{}
	a.startTicker(context.Background(), "rollup", time.Hour, func(context.Context) error { return nil })
	findJobHealth(t, a.JobHealth(), "rollup")
	if _, ok := a.SetJobEnabled("rollup", false); !ok {
		t.Fatal("registered but not-yet-leading job was reported unknown")
	}
}
