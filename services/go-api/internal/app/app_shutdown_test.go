package app

import (
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gatedStore is a behavioral-signal store whose first SatisfactionSignals call
// blocks until released, so a Service's own detached background work (driven by
// the satisfaction consumer) can be held in-flight while the shutdown drain is
// exercised.
type gatedStore struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *gatedStore) SatisfactionSignals(ctx context.Context, _ time.Time) ([]discoveryPorts.BehavioralSignal, error) {
	s.once.Do(func() { close(s.entered) })
	select {
	case <-s.release:
	case <-ctx.Done():
	}
	return nil, nil
}

// TestDrainSearchBackground_WaitsForInFlightWork is the regression for #395:
// the search service tracks detached background work (identity-bridge
// persistence, telemetry emit, vocab ingest on context.WithoutCancel) in its
// own bgWg, but *App never held a reference to it and Run()'s shutdown never
// drained it — so cleanup() closed the DB pool and Redis client while that work
// was still in flight. The drain must wait for that work before cleanup().
func TestDrainSearchBackground_WaitsForInFlightWork(t *testing.T) {
	store := &gatedStore{entered: make(chan struct{}), release: make(chan struct{})}
	svc := discoveryService.NewService(nil, discoveryService.NewCircuitBreaker(),
		discoveryService.WithBehavioralRanking(discoveryService.NewSatisfactionConsumer(store)))

	ctx, cancel := context.WithCancel(context.Background())
	svc.StartBehavioralRefresh(ctx, time.Hour)
	<-store.entered // the service now has real background work in flight

	a := &App{searchSvc: svc}

	inFlight := a.drainSearchBackground(50 * time.Millisecond)
	if inFlight.completed {
		t.Fatal("drain reported completed=true while search background work was still in flight")
	}
	if inFlight.name != "discovery search" {
		t.Errorf("outcome name: got %q, want %q", inFlight.name, "discovery search")
	}

	close(store.release) // let the blocked refresh finish
	cancel()             // stop the ticker loop so the goroutine exits

	drained := a.drainSearchBackground(2 * time.Second)
	if !drained.completed {
		t.Fatal("drain reported completed=false after search background work finished")
	}
}

// TestDrainSearchBackground_NilServiceIsClean guards the pre-setup path: an App
// with no wired search service must report a clean drain rather than panic.
func TestDrainSearchBackground_NilServiceIsClean(t *testing.T) {
	clean := (&App{}).drainSearchBackground(time.Second)
	if !clean.completed {
		t.Fatal("drain with no search service reported completed=false")
	}
	if clean.name != "discovery search" {
		t.Errorf("outcome name: got %q, want %q", clean.name, "discovery search")
	}
}

// TestShutdownComponent_TimeoutSurfacedDistinctly is the regression for #380:
// a component whose bounded shutdown exceeds its budget used to fall through to
// cleanup() identically to a clean completion, with no value returned and
// nothing logged — so the DB pool and Redis client were closed out from under
// still-running work with no signal. shutdownComponent must now report the
// timeout distinctly from a clean completion.
func TestShutdownComponent_TimeoutSurfacedDistinctly(t *testing.T) {
	a := &App{}

	started := make(chan struct{})
	slow := a.shutdownComponent("slow component", 30*time.Millisecond, func(ctx context.Context) {
		close(started)
		<-ctx.Done() // exceeds its budget; only the bounded ctx stops it
	})
	<-started
	if slow.completed {
		t.Fatal("component that exceeded its budget reported completed=true")
	}
	if slow.name != "slow component" {
		t.Errorf("outcome name: got %q, want %q", slow.name, "slow component")
	}

	fast := a.shutdownComponent("fast component", 5*time.Second, func(context.Context) {})
	if !fast.completed {
		t.Fatal("component that finished promptly reported completed=false")
	}

	// The two outcomes must be distinguishable, which is the whole point.
	if slow.completed == fast.completed {
		t.Fatal("timed-out and clean shutdowns are indistinguishable")
	}
}

func TestDrainBackground_TimeoutVsClean(t *testing.T) {
	clean := (&App{}).drainBackground(time.Second)
	if !clean.completed {
		t.Fatal("drain with no outstanding work reported completed=false")
	}

	a := &App{}
	a.wg.Add(1)
	defer a.wg.Done() // release the lingering task so the test goroutine exits cleanly

	timedOut := a.drainBackground(20 * time.Millisecond)
	if timedOut.completed {
		t.Fatal("drain that timed out with work still running reported completed=true")
	}
}

func TestUnfinishedShutdowns_NamesOnlyIncomplete(t *testing.T) {
	outcomes := []shutdownOutcome{
		{name: "alert monitor", completed: true},
		{name: "acquisition scheduler", completed: false},
		{name: "background tasks", completed: false},
	}

	got := unfinishedShutdowns(outcomes)
	want := []string{"acquisition scheduler", "background tasks"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unfinished: got %v, want %v", got, want)
	}
}

// TestShutdownComponent_LogsWarningNamingComponent confirms the timeout is not
// just returned but surfaced in the logs, naming the component that was still
// running when cleanup() would close the pool/Redis.
func TestShutdownComponent_LogsWarningNamingComponent(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(restore)

	a := &App{}
	a.shutdownComponent("acquisition scheduler", 10*time.Millisecond, func(ctx context.Context) {
		<-ctx.Done()
	})

	logged := buf.String()
	if !strings.Contains(logged, "exceeded its budget") {
		t.Errorf("expected a budget-exceeded warning, got: %q", logged)
	}
	if !strings.Contains(logged, "acquisition scheduler") {
		t.Errorf("warning did not name the stuck component, got: %q", logged)
	}
}

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
