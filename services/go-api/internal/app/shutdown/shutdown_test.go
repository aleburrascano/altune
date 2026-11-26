package shutdown_test

import (
	"altune/go-api/internal/app/shutdown"
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
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

func captureLogs(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})))
	t.Cleanup(func() { slog.SetDefault(restore) })
	return &buf
}

func outcomeOf(t *testing.T, outcomes []shutdown.Outcome, component string) shutdown.Outcome {
	t.Helper()
	for _, o := range outcomes {
		if o.Component == component {
			return o
		}
	}
	t.Fatalf("no outcome for %q in %+v", component, outcomes)
	return shutdown.Outcome{}
}

func runOne(component string, timeout time.Duration, stop func(context.Context)) shutdown.Outcome {
	return shutdown.Run(context.Background(), []shutdown.Step{{Component: component, Timeout: timeout, Stop: stop}})[0]
}

type ctxKey struct{}

func TestRun_EachStopReceivesTheCallersContextValuesWithItsOwnDeadline(t *testing.T) {
	parent := context.WithValue(context.Background(), ctxKey{}, "caller")
	var got any
	var hasDeadline bool

	shutdown.Run(parent, []shutdown.Step{{Component: "x", Timeout: time.Second, Stop: func(ctx context.Context) {
		got = ctx.Value(ctxKey{})
		_, hasDeadline = ctx.Deadline()
	}}})

	if got != "caller" || !hasDeadline {
		t.Fatalf("stop saw value %v and deadline %v, want the caller's value and a deadline", got, hasDeadline)
	}
}

func TestRun_ADetachedContextLetsAStopFinishAfterTheSignalContextIsCancelled(t *testing.T) {
	signalCtx, cancel := context.WithCancel(context.Background())
	cancel()
	detached, end := background.DetachCancelable(signalCtx)
	defer end()
	var finished bool

	outcomes := shutdown.Run(detached, []shutdown.Step{{Component: "x", Timeout: time.Second, Stop: func(ctx context.Context) {
		finished = ctx.Err() == nil
	}}})

	if outcomes[0].Status != shutdown.Completed || !finished {
		t.Fatalf("outcome = %+v, finished = %v, want the stop to finish although the signal context was cancelled", outcomes[0], finished)
	}
}

type gatedStore struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *gatedStore) Append(ctx context.Context, _ search.InteractionEvent) error {
	s.once.Do(func() { close(s.entered) })
	select {
	case <-s.release:
	case <-ctx.Done():
	}
	return nil
}

func TestDetachedTasksStep_DrainingTheTrackerWaitsForInFlightSearchWork(t *testing.T) {
	bg := searchtest.NewTracker(t)
	drain := func(ctx context.Context) { bg.Drain(ctx) }
	store := &gatedStore{entered: make(chan struct{}), release: make(chan struct{})}
	svc := searchtest.NewSearchServiceWith(t, bg, nil, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.Events = store })
	query, err := search.NewSearchQuery("humble", search.AllKinds(), 10)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, _ = svc.Execute(ctx, ids.NewUserId(uuid.New()), query)
	<-store.entered

	inFlight := runOne(shutdown.DetachedTasks, 50*time.Millisecond, drain)
	if inFlight != (shutdown.Outcome{Component: "detached request tasks", Status: shutdown.TimedOut, Budget: 50 * time.Millisecond}) {
		t.Fatalf("drain with search work in flight = %+v, want detached request tasks timed out", inFlight)
	}

	close(store.release)
	cancel()

	if drained := runOne(shutdown.DetachedTasks, 2*time.Second, drain); drained.Status != shutdown.Completed {
		t.Fatalf("drain after the search work finished = %+v, want completed", drained)
	}
}

type countingStopper struct {
	stops atomic.Int32
}

func (c *countingStopper) Shutdown(context.Context) { c.stops.Add(1) }

func TestStop_CallsTheComponentsShutdownOrNothingWhenAbsent(t *testing.T) {
	var absent *countingStopper
	if stop := shutdown.Stop(absent); stop != nil {
		t.Fatal("an absent component got a shutdown to run")
	}

	present := &countingStopper{}
	shutdown.Stop(present)(context.Background())
	if n := present.stops.Load(); n != 1 {
		t.Fatalf("Shutdown called %d times, want 1", n)
	}
}

func TestRun_ATimeoutIsSurfacedDistinctlyFromACleanStop(t *testing.T) {
	started := make(chan struct{})
	outcomes := shutdown.Run(context.Background(), []shutdown.Step{
		{Component: "slow component", Timeout: 30 * time.Millisecond, Stop: func(ctx context.Context) {
			close(started)
			<-ctx.Done()
		}},
		{Component: "fast component", Timeout: 5 * time.Second, Stop: func(context.Context) {}},
	})
	<-started

	want := []shutdown.Outcome{
		{Component: "slow component", Status: shutdown.TimedOut, Budget: 30 * time.Millisecond},
		{Component: "fast component", Status: shutdown.Completed, Budget: 5 * time.Second},
	}
	if len(outcomes) != 2 || outcomes[0] != want[0] || outcomes[1] != want[1] {
		t.Fatalf("outcomes = %+v, want %+v", outcomes, want)
	}
}

func TestRun_EveryOutcomeReportsItsStepsPlannedBudgetEvenWhenTheStepIsSkipped(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	outcomes := shutdown.Run(context.Background(), shutdown.Plan(shutdown.Components{
		Background:     func(context.Context) { <-release },
		LeaderElection: shutdown.Stop(&countingStopper{}),
	}, shutdown.Budgets{SchedulerSeconds: 20, BackgroundSeconds: 1, DetachedSeconds: 3}))

	want := map[string]time.Duration{
		shutdown.AlertMonitor:   2 * time.Second,
		shutdown.EventFeed:      2 * time.Second,
		shutdown.EvalMeter:      2 * time.Second,
		shutdown.Scheduler:      20 * time.Second,
		shutdown.Background:     1 * time.Second,
		shutdown.LeaderElection: 3 * time.Second,
		shutdown.DetachedTasks:  3 * time.Second,
	}
	if len(outcomes) != len(want) {
		t.Fatalf("got %d outcomes, want %d", len(outcomes), len(want))
	}
	for _, o := range outcomes {
		if o.Budget != want[o.Component] {
			t.Errorf("%s ran with budget %v, want %v (status %v)", o.Component, o.Budget, want[o.Component], o.Status)
		}
	}
	if got := outcomeOf(t, outcomes, shutdown.LeaderElection).Status; got != shutdown.Skipped {
		t.Errorf("leader election = %v, want Skipped after the background step timed out, and still reporting its budget", got)
	}
}

func TestRun_BackgroundDrainTimesOutOnlyWhileWorkIsRunning(t *testing.T) {
	var wg sync.WaitGroup
	wait := func(context.Context) { wg.Wait() }
	if clean := runOne(shutdown.Background, time.Second, wait); clean.Status != shutdown.Completed {
		t.Fatalf("drain with no outstanding work = %+v, want completed", clean)
	}

	wg.Add(1)
	defer wg.Done()

	if timedOut := runOne(shutdown.Background, 20*time.Millisecond, wait); timedOut.Status != shutdown.TimedOut {
		t.Fatalf("drain with work still running = %+v, want timed out", timedOut)
	}
}

func TestRun_AStopPastItsBudgetLogsAWarningNamingTheComponent(t *testing.T) {
	logged := captureLogs(t, slog.LevelWarn)

	runOne("background tasks", 10*time.Millisecond, func(ctx context.Context) { <-ctx.Done() })

	if got := logged.String(); !strings.Contains(got, "component shutdown exceeded its budget") ||
		!strings.Contains(got, `component="background tasks"`) || !strings.Contains(got, "timeout=10ms") {
		t.Errorf("warning = %q, want the budget-exceeded warning naming background tasks and its 10ms timeout", got)
	}
}

func TestRun_AStopWithinItsBudgetLogsNothing(t *testing.T) {
	logged := captureLogs(t, slog.LevelDebug)

	runOne("fast component", time.Second, func(context.Context) {})

	if got := logged.String(); got != "" {
		t.Errorf("logged %q for a clean stop, want nothing", got)
	}
}

func TestPlan_StopsComponentsInThePinnedOrder(t *testing.T) {
	want := []string{
		"alert monitor",
		"event feed",
		"eval meter",
		"acquisition scheduler",
		"background tasks",
		"leader election",
		"detached request tasks",
	}

	outcomes := shutdown.Run(context.Background(), shutdown.Plan(shutdown.Components{}, shutdown.Budgets{}))

	got := make([]string, 0, len(outcomes))
	for _, o := range outcomes {
		if o.Status != shutdown.Completed {
			t.Errorf("component %q did not complete with nothing to stop", o.Component)
		}
		got = append(got, o.Component)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("shutdown order:\n got  %v\n want %v", got, want)
	}
}

func TestPlan_RunsEachComponentsOwnStop(t *testing.T) {
	var stopped []string
	stopper := func(name string) func(context.Context) {
		return func(context.Context) { stopped = append(stopped, name) }
	}

	shutdown.Run(context.Background(), shutdown.Plan(shutdown.Components{
		AlertMonitor:   stopper("alert monitor"),
		EventFeed:      stopper("event feed"),
		EvalMeter:      stopper("eval meter"),
		Scheduler:      stopper("acquisition scheduler"),
		Background:     stopper("background tasks"),
		LeaderElection: stopper("leader election"),
		DetachedTasks:  stopper("detached request tasks"),
	}, shutdown.Budgets{}))

	want := "alert monitor,event feed,eval meter,acquisition scheduler,background tasks,leader election,detached request tasks"
	if strings.Join(stopped, ",") != want {
		t.Fatalf("stopped %v, want %s", stopped, want)
	}
}

func timeouts(plan []shutdown.Step) map[string]time.Duration {
	got := make(map[string]time.Duration, len(plan))
	for _, step := range plan {
		got[step.Component] = step.Timeout
	}
	return got
}

func TestPlan_TimeoutsPinned(t *testing.T) {
	want := map[string]time.Duration{
		"alert monitor":          2 * time.Second,
		"event feed":             2 * time.Second,
		"eval meter":             2 * time.Second,
		"acquisition scheduler":  35 * time.Second,
		"background tasks":       15 * time.Second,
		"leader election":        3 * time.Second,
		"detached request tasks": 10 * time.Second,
	}

	plan := shutdown.Plan(shutdown.Components{}, shutdown.Budgets{})
	if len(plan) != len(want) {
		t.Fatalf("plan has %d components, want %d", len(plan), len(want))
	}
	for name, timeout := range timeouts(plan) {
		if timeout != want[name] {
			t.Errorf("%s timeout: got %v, want %v", name, timeout, want[name])
		}
	}
}

const (
	totalShutdownBudget = 80 * time.Second
	stopGracePeriod     = 90 * time.Second
)

func TestPlan_BudgetsPlusServerDrainFitInsideTheTotal(t *testing.T) {
	if shutdown.ServerDrainTimeout != 10*time.Second {
		t.Fatalf("server drain timeout = %v, want 10s", shutdown.ServerDrainTimeout)
	}
	total := shutdown.ServerDrainTimeout
	for _, timeout := range timeouts(shutdown.Plan(shutdown.Components{}, shutdown.Budgets{SchedulerSeconds: 600, BackgroundSeconds: 600, DetachedSeconds: 600})) {
		total += timeout
	}
	if total > totalShutdownBudget || total >= stopGracePeriod {
		t.Fatalf("worst-case shutdown %v exceeds the %v budget inside the %v stop_grace_period", total, totalShutdownBudget, stopGracePeriod)
	}
}

func TestPlan_SchedulerTimeoutFollowsTheDrainBudgetUpToItsCap(t *testing.T) {
	for budget, want := range map[int]time.Duration{
		20:  20 * time.Second,
		35:  35 * time.Second,
		36:  35 * time.Second,
		600: 35 * time.Second,
		0:   35 * time.Second,
		-5:  35 * time.Second,
	} {
		if got := timeouts(shutdown.Plan(shutdown.Components{}, shutdown.Budgets{SchedulerSeconds: budget}))["acquisition scheduler"]; got != want {
			t.Errorf("drain budget %ds: scheduler timeout %v, want %v", budget, got, want)
		}
	}
}

func TestPlan_BackgroundAndDetachedTimeoutsFollowTheirBudgetsUpToTheirCaps(t *testing.T) {
	cases := []struct {
		background, detached         int
		wantBackground, wantDetached time.Duration
	}{
		{1, 2, 1 * time.Second, 2 * time.Second},
		{7, 3, 7 * time.Second, 3 * time.Second},
		{12, 12, 12 * time.Second, 10 * time.Second},
		{600, 600, 15 * time.Second, 10 * time.Second},
		{0, 4, 15 * time.Second, 4 * time.Second},
		{4, 0, 4 * time.Second, 10 * time.Second},
		{-5, -5, 15 * time.Second, 10 * time.Second},
	}
	for _, tc := range cases {
		plan := timeouts(shutdown.Plan(shutdown.Components{}, shutdown.Budgets{BackgroundSeconds: tc.background, DetachedSeconds: tc.detached}))
		if got := plan["background tasks"]; got != tc.wantBackground {
			t.Errorf("background %ds, detached %ds: background timeout %v, want %v", tc.background, tc.detached, got, tc.wantBackground)
		}
		if got := plan["detached request tasks"]; got != tc.wantDetached {
			t.Errorf("background %ds, detached %ds: detached timeout %v, want %v", tc.background, tc.detached, got, tc.wantDetached)
		}
	}
}

func withBackgroundBudget(plan []shutdown.Step, budget time.Duration) []shutdown.Step {
	for i := range plan {
		if plan[i].Component == shutdown.Background {
			plan[i].Timeout = budget
		}
	}
	return plan
}

func TestRun_ABackgroundDrainTimeoutKeepsTheLeaderLock(t *testing.T) {
	logged := captureLogs(t, slog.LevelInfo)
	election := &countingStopper{}
	release := make(chan struct{})
	defer close(release)
	blocked := func(context.Context) { <-release }

	outcomes := shutdown.Run(context.Background(), withBackgroundBudget(shutdown.Plan(shutdown.Components{
		Background:     blocked,
		LeaderElection: shutdown.Stop(election),
	}, shutdown.Budgets{}), 20*time.Millisecond))

	if n := election.stops.Load(); n != 0 {
		t.Fatalf("election released %d time(s) although the background drain timed out", n)
	}
	if got := outcomeOf(t, outcomes, shutdown.LeaderElection).Status; got != shutdown.Skipped {
		t.Errorf("leader election outcome = %v, want skipped so the lock is retained", got)
	}
	if got := logged.String(); !strings.Contains(got, "level=ERROR") || !strings.Contains(got, "leadership intentionally NOT released") ||
		!strings.Contains(got, `component="leader election"`) || !strings.Contains(got, `requires="background tasks"`) {
		t.Errorf("missing error log for the retained lock: %q", got)
	}
}

func TestRun_ACleanBackgroundDrainReleasesTheLeaderLock(t *testing.T) {
	election := &countingStopper{}

	outcomes := shutdown.Run(context.Background(), shutdown.Plan(shutdown.Components{
		Background:     func(context.Context) {},
		LeaderElection: shutdown.Stop(election),
	}, shutdown.Budgets{}))

	if n := election.stops.Load(); n != 1 {
		t.Fatalf("election released %d time(s) after a clean drain, want 1", n)
	}
	if got := outcomeOf(t, outcomes, shutdown.LeaderElection).Status; got != shutdown.Completed {
		t.Errorf("leader election outcome = %v after a clean drain, want completed", got)
	}
}

func TestDrainServer_AHungHandlerIsClosedAfterTheDeadline(t *testing.T) {
	logged := captureLogs(t, slog.LevelDebug)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
	}))
	go func() {
		if resp, err := http.Get(srv.URL); err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-started

	returned := make(chan struct{})
	go func() { shutdown.DrainServer(context.Background(), srv.Config, 50*time.Millisecond); close(returned) }()
	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Fatal("DrainServer did not return with a hung handler")
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight handler was not cancelled at drain expiry")
	}
	if got := logged.String(); !strings.Contains(got, "server shutdown error") || strings.Contains(got, "server close error") {
		t.Errorf("forced drain logged %q, want the shutdown error and no close error, since the close succeeded", got)
	}
}

func TestDrainServer_AnIdleServerStopsWithoutBeingForced(t *testing.T) {
	logged := captureLogs(t, slog.LevelDebug)
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	shutdown.DrainServer(context.Background(), srv.Config, time.Second)

	if got := logged.String(); got != "" {
		t.Errorf("idle drain logged %q, want nothing", got)
	}
	if resp, err := http.Get(srv.URL); err == nil {
		_ = resp.Body.Close()
		t.Error("server still answers after the drain")
	}
}
