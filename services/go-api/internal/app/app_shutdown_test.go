package app

import (
	discoveryDomain "altune/go-api/internal/discovery/domain"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

type gatedStore struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *gatedStore) Append(ctx context.Context, _ discoveryDomain.InteractionEvent) error {
	s.once.Do(func() { close(s.entered) })
	select {
	case <-s.release:
	case <-ctx.Done():
	}
	return nil
}

func TestDrainSearchBackground_WaitsForInFlightWork(t *testing.T) {
	store := &gatedStore{entered: make(chan struct{}), release: make(chan struct{})}
	svc := discoveryService.NewService(nil, discoveryService.NewCircuitBreaker(),
		discoveryService.WithEventStore(store))

	query, err := discoveryDomain.NewSearchQuery("humble", discoveryDomain.AllKinds(), 10)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, _ = svc.Execute(ctx, shared.NewUserId(uuid.New()), query, false)
	<-store.entered

	a := &App{searchSvc: svc}

	inFlight := a.shutdownComponent(discoverySearchComponent, 50*time.Millisecond, a.waitSearchBackground)
	if inFlight.status != shutdownTimedOut {
		t.Fatal("drain reported a non-timed-out status while search background work was still in flight")
	}
	if inFlight.name != "discovery search" {
		t.Errorf("outcome name: got %q, want %q", inFlight.name, "discovery search")
	}

	close(store.release)
	cancel()

	drained := a.shutdownComponent(discoverySearchComponent, 2*time.Second, a.waitSearchBackground)
	if drained.status != shutdownCompleted {
		t.Fatal("drain reported completed=false after search background work finished")
	}
}

func TestDrainSearchBackground_NilServiceIsClean(t *testing.T) {
	a := &App{}
	clean := a.shutdownComponent(discoverySearchComponent, time.Second, a.waitSearchBackground)
	if clean.status != shutdownCompleted {
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
	if slow.status != shutdownTimedOut {
		t.Fatal("component that exceeded its budget reported completed=true")
	}
	if slow.name != "slow component" {
		t.Errorf("outcome name: got %q, want %q", slow.name, "slow component")
	}

	fast := a.shutdownComponent("fast component", 5*time.Second, func(context.Context) {})
	if fast.status != shutdownCompleted {
		t.Fatal("component that finished promptly reported completed=false")
	}

	if slow.status == fast.status {
		t.Fatal("timed-out and clean shutdowns are indistinguishable")
	}
}

func TestDrainBackground_TimeoutVsClean(t *testing.T) {
	a := &App{}
	clean := a.shutdownComponent(backgroundTasksComponent, time.Second, a.waitBackground)
	if clean.status != shutdownCompleted {
		t.Fatal("drain with no outstanding work reported completed=false")
	}

	a.wg.Add(1)
	defer a.wg.Done()

	timedOut := a.shutdownComponent(backgroundTasksComponent, 20*time.Millisecond, a.waitBackground)
	if timedOut.status != shutdownTimedOut {
		t.Fatal("drain that timed out with work still running reported completed=true")
	}
}

func TestUnfinishedShutdowns_NamesOnlyIncomplete(t *testing.T) {
	outcomes := []shutdownOutcome{
		{name: "alert monitor", status: shutdownCompleted},
		{name: "acquisition scheduler", status: shutdownTimedOut},
		{name: "background tasks", status: shutdownTimedOut},
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
		if o.status != shutdownCompleted {
			t.Errorf("component %q did not complete on an empty App", o.name)
		}
		got = append(got, string(o.name))
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("shutdown order:\n got  %v\n want %v", got, want)
	}
}

func TestShutdownPlan_TimeoutsPinned(t *testing.T) {
	want := map[componentName]time.Duration{
		"alert monitor":         2 * time.Second,
		"event feed":            2 * time.Second,
		"eval meter":            2 * time.Second,
		"acquisition scheduler": 35 * time.Second,
		"background tasks":      15 * time.Second,
		"leader election":       3 * time.Second,
		"discovery search":      10 * time.Second,
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
	a.shutdownComponent(backgroundTasksComponent, 10*time.Millisecond, a.waitBackground)

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

func TestShutdownPlan_BudgetsPlusServerDrainFitInsideTotal(t *testing.T) {
	total := serverDrainTimeout
	for _, c := range (&App{cfg: &config.Config{AcquisitionDrainBudgetSeconds: 600}}).shutdownPlan() {
		total += c.timeout
	}
	if total > shutdownTotalBudget {
		t.Fatalf("worst-case shutdown %v exceeds total budget %v", total, shutdownTotalBudget)
	}
	if shutdownTotalBudget >= 90*time.Second {
		t.Fatalf("total budget %v must stay under the 90s stop_grace_period", shutdownTotalBudget)
	}
}

func TestShutdownPlan_SchedulerTimeoutFollowsDrainBudget(t *testing.T) {
	timeoutFor := func(seconds int) time.Duration {
		plan := (&App{cfg: &config.Config{AcquisitionDrainBudgetSeconds: seconds}}).shutdownPlan()
		for _, c := range plan {
			if c.name == "acquisition scheduler" {
				return c.timeout
			}
		}
		t.Fatal("no scheduler component")
		return 0
	}
	if got := timeoutFor(20); got != 20*time.Second {
		t.Errorf("drain budget 20s: scheduler timeout %v, want 20s", got)
	}
	if got := timeoutFor(600); got != schedulerDrainCap {
		t.Errorf("drain budget 600s: scheduler timeout %v, want cap %v", got, schedulerDrainCap)
	}
}

func TestDrainServer_HungHandlerIsClosedAfterDeadline(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
	}))
	srv.Start()
	a := &App{server: srv.Config}

	go func() {
		if resp, err := http.Get(srv.URL); err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-started

	returned := make(chan struct{})
	go func() { a.drainServer(50 * time.Millisecond); close(returned) }()
	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Fatal("drainServer did not return with a hung handler")
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight handler was not cancelled at drain expiry")
	}
}
