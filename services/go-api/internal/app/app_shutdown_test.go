package app_test

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/app/shutdown"
	"context"
	"testing"
	"time"
)

const allShutdownSteps = 7

func byComponent(outcomes []shutdown.Outcome) map[string]shutdown.Outcome {
	got := make(map[string]shutdown.Outcome, len(outcomes))
	for _, o := range outcomes {
		got[o.Component] = o
	}
	return got
}

func TestAssemblyShutdown_ADetachedTaskHeldOpenTimesOutOnlyTheDetachedStep(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.BackgroundDrainBudgetSeconds = 5
	cfg.DetachedDrainBudgetSeconds = 1
	tracker := testTracker(t)
	held := make(chan struct{})
	started := make(chan struct{})
	t.Cleanup(func() { close(held) })
	if !tracker.LaunchWithin(t.Context(), "held-for-shutdown-test", time.Minute, func(context.Context) {
		close(started)
		<-held
	}) {
		t.Fatal("the tracker refused the held task")
	}
	<-started
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	assembly, err := app.Assemble(ctx, cfg, app.Handles{Pool: unreachablePool(t), Transport: refuseEgress{}, Tracker: tracker})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	cancel()
	outcomes := assembly.Shutdown(context.Background())

	if len(outcomes) != allShutdownSteps {
		t.Fatalf("Shutdown reported %d steps, want %d", len(outcomes), allShutdownSteps)
	}
	got := byComponent(outcomes)
	if got[shutdown.DetachedTasks].Status != shutdown.TimedOut {
		t.Errorf("%q = %v, want TimedOut: the held task is drained by the detached step", shutdown.DetachedTasks, got[shutdown.DetachedTasks].Status)
	}
	for _, name := range []string{shutdown.AlertMonitor, shutdown.EventFeed, shutdown.EvalMeter, shutdown.Scheduler, shutdown.Background, shutdown.LeaderElection} {
		if got[name].Status != shutdown.Completed {
			t.Errorf("%q = %v, want Completed: it does not wait on detached tasks", name, got[name].Status)
		}
	}
	if b := got[shutdown.DetachedTasks].Budget; b != 1*time.Second {
		t.Errorf("%q ran with budget %v, want DETACHED_DRAIN_BUDGET_SECONDS (1s)", shutdown.DetachedTasks, b)
	}
	if b := got[shutdown.Background].Budget; b != 5*time.Second {
		t.Errorf("%q ran with budget %v, want BACKGROUND_DRAIN_BUDGET_SECONDS (5s)", shutdown.Background, b)
	}
}

func TestAssemblyShutdown_JobLoopsStillRunningTimeOutOnlyTheBackgroundStepAndKeepTheLeaderLock(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.BackgroundDrainBudgetSeconds = 1
	cfg.DetachedDrainBudgetSeconds = 5
	ctx, cancel := context.WithCancel(t.Context())
	assembly, err := app.Assemble(ctx, cfg, app.Handles{Pool: unreachablePool(t), Transport: refuseEgress{}, Tracker: testTracker(t)})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		assembly.Jobs().Wait()
	})

	outcomes := assembly.Shutdown(context.Background())

	if len(outcomes) != allShutdownSteps {
		t.Fatalf("Shutdown reported %d steps, want %d", len(outcomes), allShutdownSteps)
	}
	got := byComponent(outcomes)
	if got[shutdown.Background].Status != shutdown.TimedOut {
		t.Errorf("%q = %v, want TimedOut: the job loops were never cancelled", shutdown.Background, got[shutdown.Background].Status)
	}
	if got[shutdown.LeaderElection].Status != shutdown.Skipped {
		t.Errorf("%q = %v, want Skipped: the lock is kept while jobs may still run", shutdown.LeaderElection, got[shutdown.LeaderElection].Status)
	}
	if got[shutdown.DetachedTasks].Status != shutdown.Completed {
		t.Errorf("%q = %v, want Completed: no detached task is held", shutdown.DetachedTasks, got[shutdown.DetachedTasks].Status)
	}
	if b := got[shutdown.Background].Budget; b != 1*time.Second {
		t.Errorf("%q ran with budget %v, want BACKGROUND_DRAIN_BUDGET_SECONDS (1s)", shutdown.Background, b)
	}
	if b := got[shutdown.DetachedTasks].Budget; b != 5*time.Second {
		t.Errorf("%q ran with budget %v, want DETACHED_DRAIN_BUDGET_SECONDS (5s)", shutdown.DetachedTasks, b)
	}
}
