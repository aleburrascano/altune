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

func TestDrainSearchBackground_WaitsForInFlightWork(t *testing.T) {
	store := &gatedStore{entered: make(chan struct{}), release: make(chan struct{})}
	svc := discoveryService.NewService(nil, discoveryService.NewCircuitBreaker(),
		discoveryService.WithBehavioralRanking(discoveryService.NewSatisfactionConsumer(store)))

	ctx, cancel := context.WithCancel(context.Background())
	svc.StartBehavioralRefresh(ctx, time.Hour)
	<-store.entered

	a := &App{searchSvc: svc}

	inFlight := a.drainSearchBackground(50 * time.Millisecond)
	if inFlight.completed {
		t.Fatal("drain reported completed=true while search background work was still in flight")
	}
	if inFlight.name != "discovery search" {
		t.Errorf("outcome name: got %q, want %q", inFlight.name, "discovery search")
	}

	close(store.release)
	cancel()

	drained := a.drainSearchBackground(2 * time.Second)
	if !drained.completed {
		t.Fatal("drain reported completed=false after search background work finished")
	}
}

func TestDrainSearchBackground_NilServiceIsClean(t *testing.T) {
	clean := (&App{}).drainSearchBackground(time.Second)
	if !clean.completed {
		t.Fatal("drain with no search service reported completed=false")
	}
	if clean.name != "discovery search" {
		t.Errorf("outcome name: got %q, want %q", clean.name, "discovery search")
	}
}

func TestShutdownComponent_TimeoutSurfacedDistinctly(t *testing.T) {
	a := &App{}

	started := make(chan struct{})
	slow := a.shutdownComponent("slow component", 30*time.Millisecond, func(ctx context.Context) {
		close(started)
		<-ctx.Done()
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
	defer a.wg.Done()

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

func TestShutdownPlan_TimeoutsPinned(t *testing.T) {
	want := map[string]time.Duration{
		"alert monitor":         5 * time.Second,
		"event feed":            5 * time.Second,
		"eval meter":            5 * time.Second,
		"acquisition scheduler": 70 * time.Second,
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

type countingElection struct {
	fakeElection
	shutdowns atomic.Int32
}

func (c *countingElection) Shutdown(context.Context) { c.shutdowns.Add(1) }

func planWithDrainBudget(a *App, budget time.Duration) []componentShutdown {
	plan := a.shutdownPlan()
	for i := range plan {
		if plan[i].name == backgroundTasksComponent {
			plan[i].timeout = budget
		}
	}
	return plan
}

func TestShutdown_DrainTimeout_KeepsLeaderLock(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(restore)

	election := &countingElection{}
	a := &App{election: election}
	release := make(chan struct{})
	a.wg.Add(1)
	go func() { defer a.wg.Done(); <-release }()
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
