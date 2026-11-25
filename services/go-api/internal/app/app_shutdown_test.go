package app_test

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/app/shutdown"
	"context"
	"testing"
	"time"
)

const allShutdownSteps = 7

func statuses(outcomes []shutdown.Outcome) map[string]shutdown.Status {
	got := make(map[string]shutdown.Status, len(outcomes))
	for _, o := range outcomes {
		got[o.Component] = o.Status
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
	begun := time.Now()
	outcomes := assembly.Shutdown()
	elapsed := time.Since(begun)

	if len(outcomes) != allShutdownSteps {
		t.Fatalf("Shutdown reported %d steps, want %d", len(outcomes), allShutdownSteps)
	}
	got := statuses(outcomes)
	if got[shutdown.DetachedTasks] != shutdown.TimedOut {
		t.Errorf("%q = %v, want TimedOut: the held task is drained by the detached step", shutdown.DetachedTasks, got[shutdown.DetachedTasks])
	}
	for _, name := range []string{shutdown.AlertMonitor, shutdown.EventFeed, shutdown.EvalMeter, shutdown.Scheduler, shutdown.Background, shutdown.LeaderElection} {
		if got[name] != shutdown.Completed {
			t.Errorf("%q = %v, want Completed: it does not wait on detached tasks", name, got[name])
		}
	}
	if elapsed >= 4*time.Second {
		t.Errorf("Shutdown took %v, want about the detached budget of 1s: the detached step must use DETACHED_DRAIN_BUDGET_SECONDS, not the background one or its cap", elapsed)
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

	begun := time.Now()
	outcomes := assembly.Shutdown()
	elapsed := time.Since(begun)

	if len(outcomes) != allShutdownSteps {
		t.Fatalf("Shutdown reported %d steps, want %d", len(outcomes), allShutdownSteps)
	}
	got := statuses(outcomes)
	if got[shutdown.Background] != shutdown.TimedOut {
		t.Errorf("%q = %v, want TimedOut: the job loops were never cancelled", shutdown.Background, got[shutdown.Background])
	}
	if got[shutdown.LeaderElection] != shutdown.Skipped {
		t.Errorf("%q = %v, want Skipped: the lock is kept while jobs may still run", shutdown.LeaderElection, got[shutdown.LeaderElection])
	}
	if got[shutdown.DetachedTasks] != shutdown.Completed {
		t.Errorf("%q = %v, want Completed: no detached task is held", shutdown.DetachedTasks, got[shutdown.DetachedTasks])
	}
	if elapsed >= 4*time.Second {
		t.Errorf("Shutdown took %v, want about the background budget of 1s: the background step must use BACKGROUND_DRAIN_BUDGET_SECONDS, not the detached one or its cap", elapsed)
	}
}
