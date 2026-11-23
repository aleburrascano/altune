package shutdown_test

import (
	"altune/go-api/internal/app/shutdown"
	"altune/go-api/internal/discovery"
	shared "altune/go-api/internal/shared/kernel"
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

func runOne(component string, timeout time.Duration, stop func(context.Context)) shutdown.Outcome {
	return shutdown.Run([]shutdown.Step{{Component: component, Timeout: timeout, Stop: stop}})[0]
}

type gatedStore struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *gatedStore) Append(ctx context.Context, _ discovery.InteractionEvent) error {
	s.once.Do(func() { close(s.entered) })
	select {
	case <-s.release:
	case <-ctx.Done():
	}
	return nil
}

func TestWait_DiscoverySearchDrainWaitsForInFlightWork(t *testing.T) {
	store := &gatedStore{entered: make(chan struct{}), release: make(chan struct{})}
	svc := discovery.NewSearchService(nil, discovery.NewCircuitBreaker(), discovery.WithEventStore(store))
	query, err := discovery.NewSearchQuery("humble", discovery.AllKinds(), 10)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, _ = svc.Execute(ctx, shared.NewUserId(uuid.New()), query, false)
	<-store.entered

	inFlight := runOne(shutdown.DiscoverySearch, 50*time.Millisecond, shutdown.Wait(svc))
	if inFlight != (shutdown.Outcome{Component: "discovery search", Status: shutdown.TimedOut}) {
		t.Fatalf("drain with search work in flight = %+v, want discovery search timed out", inFlight)
	}

	close(store.release)
	cancel()

	if drained := runOne(shutdown.DiscoverySearch, 2*time.Second, shutdown.Wait(svc)); drained.Status != shutdown.Completed {
		t.Fatalf("drain after the search work finished = %+v, want completed", drained)
	}
}

func TestWait_AnAbsentServiceHasNothingToDrain(t *testing.T) {
	var svc *discovery.SearchService

	if stop := shutdown.Wait(svc); stop != nil {
		t.Fatal("an absent search service got a drain to run")
	}
	if clean := runOne(shutdown.DiscoverySearch, time.Second, shutdown.Wait(svc)); clean != (shutdown.Outcome{Component: "discovery search", Status: shutdown.Completed}) {
		t.Fatalf("outcome = %+v, want discovery search completed", clean)
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
	outcomes := shutdown.Run([]shutdown.Step{
		{Component: "slow component", Timeout: 30 * time.Millisecond, Stop: func(ctx context.Context) {
			close(started)
			<-ctx.Done()
		}},
		{Component: "fast component", Timeout: 5 * time.Second, Stop: func(context.Context) {}},
	})
	<-started

	want := []shutdown.Outcome{
		{Component: "slow component", Status: shutdown.TimedOut},
		{Component: "fast component", Status: shutdown.Completed},
	}
	if len(outcomes) != 2 || outcomes[0] != want[0] || outcomes[1] != want[1] {
		t.Fatalf("outcomes = %+v, want %+v", outcomes, want)
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

func TestUnfinished_NamesOnlyTheComponentsThatDidNotComplete(t *testing.T) {
	got := shutdown.Unfinished([]shutdown.Outcome{
		{Component: "alert monitor", Status: shutdown.Completed},
		{Component: "acquisition scheduler", Status: shutdown.TimedOut},
		{Component: "leader election", Status: shutdown.Skipped},
	})

	if want := "acquisition scheduler,leader election"; strings.Join(got, ",") != want {
		t.Fatalf("unfinished = %v, want %s", got, want)
	}
	if got := shutdown.Unfinished([]shutdown.Outcome{{Component: "alert monitor", Status: shutdown.Completed}}); got != nil {
		t.Errorf("unfinished with every component done = %v, want none", got)
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
		"discovery search",
	}

	outcomes := shutdown.Run(shutdown.Plan(shutdown.Components{}, 0))

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

	shutdown.Run(shutdown.Plan(shutdown.Components{
		AlertMonitor:    stopper("alert monitor"),
		EventFeed:       stopper("event feed"),
		EvalMeter:       stopper("eval meter"),
		Scheduler:       stopper("acquisition scheduler"),
		Background:      stopper("background tasks"),
		LeaderElection:  stopper("leader election"),
		DiscoverySearch: stopper("discovery search"),
	}, 0))

	want := "alert monitor,event feed,eval meter,acquisition scheduler,background tasks,leader election,discovery search"
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
		"alert monitor":         2 * time.Second,
		"event feed":            2 * time.Second,
		"eval meter":            2 * time.Second,
		"acquisition scheduler": 35 * time.Second,
		"background tasks":      15 * time.Second,
		"leader election":       3 * time.Second,
		"discovery search":      10 * time.Second,
	}

	plan := shutdown.Plan(shutdown.Components{}, 0)
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
	for _, timeout := range timeouts(shutdown.Plan(shutdown.Components{}, 600)) {
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
		if got := timeouts(shutdown.Plan(shutdown.Components{}, budget))["acquisition scheduler"]; got != want {
			t.Errorf("drain budget %ds: scheduler timeout %v, want %v", budget, got, want)
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

	outcomes := shutdown.Run(withBackgroundBudget(shutdown.Plan(shutdown.Components{
		Background:     blocked,
		LeaderElection: shutdown.Stop(election),
	}, 0), 20*time.Millisecond))

	if n := election.stops.Load(); n != 0 {
		t.Fatalf("election released %d time(s) although the background drain timed out", n)
	}
	if !shutdown.LeadershipRetained(outcomes) {
		t.Error("outcomes do not report leadership as retained")
	}
	if got := logged.String(); !strings.Contains(got, "level=ERROR") || !strings.Contains(got, "leadership intentionally NOT released") ||
		!strings.Contains(got, `component="leader election"`) || !strings.Contains(got, `requires="background tasks"`) {
		t.Errorf("missing error log for the retained lock: %q", got)
	}
}

func TestRun_ACleanBackgroundDrainReleasesTheLeaderLock(t *testing.T) {
	election := &countingStopper{}

	outcomes := shutdown.Run(shutdown.Plan(shutdown.Components{
		Background:     func(context.Context) {},
		LeaderElection: shutdown.Stop(election),
	}, 0))

	if n := election.stops.Load(); n != 1 {
		t.Fatalf("election released %d time(s) after a clean drain, want 1", n)
	}
	if shutdown.LeadershipRetained(outcomes) {
		t.Error("clean drain reported leadership as retained")
	}
}

func TestLeadershipRetained_OnlyASkippedLeaderElectionRetainsIt(t *testing.T) {
	cases := map[string]struct {
		outcomes []shutdown.Outcome
		want     bool
	}{
		"skipped election":     {[]shutdown.Outcome{{Component: "leader election", Status: shutdown.Skipped}}, true},
		"timed out election":   {[]shutdown.Outcome{{Component: "leader election", Status: shutdown.TimedOut}}, false},
		"another skipped step": {[]shutdown.Outcome{{Component: "background tasks", Status: shutdown.Skipped}}, false},
		"no outcomes":          {nil, false},
		"completed election":   {[]shutdown.Outcome{{Component: "leader election", Status: shutdown.Completed}}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := shutdown.LeadershipRetained(tc.outcomes); got != tc.want {
				t.Errorf("LeadershipRetained = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDrainServer_AHungHandlerIsClosedAfterTheDeadline(t *testing.T) {
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
	go func() { shutdown.DrainServer(srv.Config, 50*time.Millisecond); close(returned) }()
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
}

func TestDrainServer_AnIdleServerStopsWithoutBeingForced(t *testing.T) {
	logged := captureLogs(t, slog.LevelDebug)
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	shutdown.DrainServer(srv.Config, time.Second)

	if got := logged.String(); got != "" {
		t.Errorf("idle drain logged %q, want nothing", got)
	}
	if _, err := http.Get(srv.URL); err == nil {
		t.Error("server still answers after the drain")
	}
}
