package app

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestRunShutdownSequence_OrderPinned pins the shutdown order: the background
// drain must precede the leader-election release (so the next leader cannot
// start duplicate jobs), and the search drain runs last before cleanup().
func TestRunShutdownSequence_OrderPinned(t *testing.T) {
	want := []string{
		"alert monitor",
		"event feed",
		"eval meter",
		"acquisition scheduler",
		"background tasks",
		"leader election",
		"discovery search",
	}

	outcomes := (&App{}).runShutdownSequence()

	got := make([]string, 0, len(outcomes))
	for _, o := range outcomes {
		if !o.completed {
			t.Errorf("component %q did not complete on an empty App", o.name)
		}
		got = append(got, o.name)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("shutdown order:\n got  %v\n want %v", got, want)
	}
}

// TestShutdownPlan_TimeoutsPinned pins each component's shutdown budget.
func TestShutdownPlan_TimeoutsPinned(t *testing.T) {
	want := map[string]time.Duration{
		"alert monitor":         5 * time.Second,
		"event feed":            5 * time.Second,
		"eval meter":            5 * time.Second,
		"acquisition scheduler": 30 * time.Second,
		"background tasks":      30 * time.Second,
		"leader election":       5 * time.Second,
		"discovery search":      30 * time.Second,
	}

	plan := (&App{}).shutdownPlan()
	if len(plan) != len(want) {
		t.Fatalf("plan has %d components, want %d", len(plan), len(want))
	}
	for _, c := range plan {
		if c.timeout != want[c.name] {
			t.Errorf("%s timeout: got %v, want %v", c.name, c.timeout, want[c.name])
		}
	}
}

// TestDrains_LogSharedBudgetWarning confirms both drains now emit
// shutdownComponent's warning shape, naming the stuck component.
func TestDrains_LogSharedBudgetWarning(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(restore)

	a := &App{}
	a.wg.Add(1)
	defer a.wg.Done()
	a.drainBackground(10 * time.Millisecond)

	logged := buf.String()
	if !strings.Contains(logged, "component shutdown exceeded its budget") ||
		!strings.Contains(logged, `component="background tasks"`) {
		t.Errorf("drain warning does not match shutdownComponent's shape: %q", logged)
	}
}

// countingElection records whether the advisory lock was released.
type countingElection struct {
	fakeElection
	shutdowns atomic.Int32
}

func (c *countingElection) Shutdown(context.Context) { c.shutdowns.Add(1) }

// planWithDrainBudget returns the real shutdown plan with the background drain's
// budget shrunk so a test can drive a job past the deadline quickly.
func planWithDrainBudget(a *App, budget time.Duration) []componentShutdown {
	plan := a.shutdownPlan()
	for i := range plan {
		if plan[i].name == backgroundTasksComponent {
			plan[i].timeout = budget
		}
	}
	return plan
}

// TestShutdown_DrainTimeout_KeepsLeaderLock is the regression for #1012: when a
// leader-only background job outlives the drain budget, the leader-election
// lock must NOT be released, or the next instance wins leadership and runs the
// same job concurrently with the still-running one.
func TestShutdown_DrainTimeout_KeepsLeaderLock(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(restore)

	election := &countingElection{}
	a := &App{election: election}
	release := make(chan struct{})
	a.wg.Add(1)
	go func() { defer a.wg.Done(); <-release }() // a job stuck past the deadline
	defer close(release)

	outcomes := a.runShutdownPlan(planWithDrainBudget(a, 20*time.Millisecond))

	if n := election.shutdowns.Load(); n != 0 {
		t.Fatalf("election released %d time(s) although the background drain timed out", n)
	}
	if !leadershipRetained(outcomes) {
		t.Error("outcomes do not report leadership as retained")
	}
	logged := buf.String()
	if !strings.Contains(logged, "level=ERROR") || !strings.Contains(logged, "leadership intentionally NOT released") {
		t.Errorf("missing error log for the retained lock: %q", logged)
	}
}

// TestShutdown_DrainCompletes_ReleasesLeaderLock pins the happy path: a clean
// drain still releases the lock exactly once.
func TestShutdown_DrainCompletes_ReleasesLeaderLock(t *testing.T) {
	election := &countingElection{}
	a := &App{election: election}

	outcomes := a.runShutdownPlan(planWithDrainBudget(a, time.Second))

	if n := election.shutdowns.Load(); n != 1 {
		t.Fatalf("election released %d time(s) after a clean drain, want 1", n)
	}
	if leadershipRetained(outcomes) {
		t.Error("clean drain reported leadership as retained")
	}
}
