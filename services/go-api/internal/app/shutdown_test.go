package app_test

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

func captureLogsAt(t *testing.T, level slog.Level) *logging.Capture {
	t.Helper()
	return logging.CaptureText(t, level)
}

func outcomeOf(t *testing.T, outcomes []app.StopOutcome, component string) app.StopOutcome {
	t.Helper()
	for _, o := range outcomes {
		if o.Component == component {
			return o
		}
	}
	t.Fatalf("no outcome for %q in %+v", component, outcomes)
	return app.StopOutcome{}
}

func runOne(component string, timeout time.Duration, stop func(context.Context)) app.StopOutcome {
	return app.RunShutdown(context.Background(), []app.StopStep{{Component: component, Timeout: timeout, Stop: stop}})[0]
}

type ctxKey struct{}

func TestRun_EachStopReceivesTheCallersContextValuesWithItsOwnDeadline(t *testing.T) {
	parent := context.WithValue(context.Background(), ctxKey{}, "caller")
	var got any
	var hasDeadline bool
	app.RunShutdown(parent, []app.StopStep{{Component: "x", Timeout: time.Second, Stop: func(ctx context.Context) {
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

	outcomes := app.RunShutdown(detached, []app.StopStep{{Component: "x", Timeout: time.Second, Stop: func(ctx context.Context) {
		finished = ctx.Err() == nil
	}}})

	if outcomes[0].Status != app.StopCompleted || !finished {
		t.Fatalf("outcome = %+v, finished = %v, want the stop to finish although the signal context was cancelled", outcomes[0], finished)
	}
}

type gatedStore struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *gatedStore) Append(ctx context.Context, _ searchtest.InteractionEvent) error {
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
	svc := searchtest.NewSearchServiceWith(t, bg, nil, searchtest.NewCircuitBreaker(clock.Now), func(c *searchtest.Collaborators) { c.Events = store })
	query, err := search.NewSearchQuery("humble", searchtest.AllKinds(), 10)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, _ = svc.Execute(ctx, ids.NewUserId(uuid.New()), query)
	<-store.entered

	inFlight := runOne(app.StopDetachedTasks, 50*time.Millisecond, drain)
	if inFlight != (app.StopOutcome{Component: "detached request tasks", Status: app.StopTimedOut, Budget: 50 * time.Millisecond}) {
		t.Fatalf("drain with search work in flight = %+v, want detached request tasks timed out", inFlight)
	}

	close(store.release)
	cancel()

	if drained := runOne(app.StopDetachedTasks, 2*time.Second, drain); drained.Status != app.StopCompleted {
		t.Fatalf("drain after the search work finished = %+v, want completed", drained)
	}
}

type countingStopper struct {
	stops atomic.Int32
}

func (c *countingStopper) Shutdown(context.Context) { c.stops.Add(1) }

func TestStop_CallsTheComponentsShutdownOrNothingWhenAbsent(t *testing.T) {
	var absent *countingStopper
	if stop := app.StopOf(absent); stop != nil {
		t.Fatal("an absent component got a shutdown to run")
	}

	present := &countingStopper{}
	app.StopOf(present)(context.Background())
	if n := present.stops.Load(); n != 1 {
		t.Fatalf("Shutdown called %d times, want 1", n)
	}
}

func TestRun_ATimeoutIsSurfacedDistinctlyFromACleanStop(t *testing.T) {
	started := make(chan struct{})
	outcomes := app.RunShutdown(context.Background(), []app.StopStep{
		{Component: "slow component", Timeout: 30 * time.Millisecond, Stop: func(ctx context.Context) {
			close(started)
			<-ctx.Done()
		}},
		{Component: "fast component", Timeout: 5 * time.Second, Stop: func(context.Context) {}},
	})
	<-started

	want := []app.StopOutcome{
		{Component: "slow component", Status: app.StopTimedOut, Budget: 30 * time.Millisecond},
		{Component: "fast component", Status: app.StopCompleted, Budget: 5 * time.Second},
	}
	if len(outcomes) != 2 || outcomes[0] != want[0] || outcomes[1] != want[1] {
		t.Fatalf("outcomes = %+v, want %+v", outcomes, want)
	}
}

func TestRun_EveryOutcomeReportsItsStepsPlannedBudgetEvenWhenTheStepIsSkipped(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	outcomes := app.RunShutdown(context.Background(), app.PlanShutdown(app.StopComponents{
		Background:     func(context.Context) { <-release },
		LeaderElection: app.StopOf(&countingStopper{}),
	}, app.StopBudgets{SchedulerSeconds: 20, BackgroundSeconds: 1, DetachedSeconds: 3}))

	want := map[string]time.Duration{app.StopEventFeed: 2 * time.Second, app.StopScheduler: 20 * time.Second, app.StopBackground: 1 * time.Second, app.StopLeaderElection: 3 * time.Second, app.StopDetachedTasks: 3 * time.Second}
	if len(outcomes) != len(want) {
		t.Fatalf("got %d outcomes, want %d", len(outcomes), len(want))
	}
	for _, o := range outcomes {
		if o.Budget != want[o.Component] {
			t.Errorf("%s ran with budget %v, want %v (status %v)", o.Component, o.Budget, want[o.Component], o.Status)
		}
	}
	if got := outcomeOf(t, outcomes, app.StopLeaderElection).Status; got != app.StopSkipped {
		t.Errorf("leader election = %v, want Skipped after the background step timed out, and still reporting its budget", got)
	}
}

func TestRun_BackgroundDrainTimesOutOnlyWhileWorkIsRunning(t *testing.T) {
	var wg sync.WaitGroup
	wait := func(context.Context) { wg.Wait() }
	if clean := runOne(app.StopBackground, time.Second, wait); clean.Status != app.StopCompleted {
		t.Fatalf("drain with no outstanding work = %+v, want completed", clean)
	}

	wg.Add(1)
	defer wg.Done()

	if timedOut := runOne(app.StopBackground, 20*time.Millisecond, wait); timedOut.Status != app.StopTimedOut {
		t.Fatalf("drain with work still running = %+v, want timed out", timedOut)
	}
}

func TestRun_AStopPastItsBudgetLogsAWarningNamingTheComponent(t *testing.T) {
	logged := captureLogsAt(t, slog.LevelWarn)

	runOne("background tasks", 10*time.Millisecond, func(ctx context.Context) { <-ctx.Done() })

	if got := logged.String(); !strings.Contains(got, "component shutdown exceeded its budget") ||
		!strings.Contains(got, `component="background tasks"`) || !strings.Contains(got, "timeout=10ms") {
		t.Errorf("warning = %q, want the budget-exceeded warning naming background tasks and its 10ms timeout", got)
	}
}

func TestRun_AStopWithinItsBudgetLogsNothing(t *testing.T) {
	logged := captureLogsAt(t, slog.LevelDebug)

	runOne("fast component", time.Second, func(context.Context) {})

	if got := logged.String(); got != "" {
		t.Errorf("logged %q for a clean stop, want nothing", got)
	}
}

func TestPlan_StopsComponentsInThePinnedOrder(t *testing.T) {
	want := []string{
		"event feed",
		"acquisition scheduler",
		"background tasks",
		"leader election",
		"detached request tasks",
	}

	outcomes := app.RunShutdown(context.Background(), app.PlanShutdown(app.StopComponents{}, app.StopBudgets{}))

	got := make([]string, 0, len(outcomes))
	for _, o := range outcomes {
		if o.Status != app.StopCompleted {
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
	app.RunShutdown(context.Background(), app.PlanShutdown(app.StopComponents{
		EventFeed:      stopper("event feed"),
		Scheduler:      stopper("acquisition scheduler"),
		Background:     stopper("background tasks"),
		LeaderElection: stopper("leader election"),
		DetachedTasks:  stopper("detached request tasks"),
	}, app.StopBudgets{}))

	want := "event feed,acquisition scheduler,background tasks,leader election,detached request tasks"
	if strings.Join(stopped, ",") != want {
		t.Fatalf("stopped %v, want %s", stopped, want)
	}
}

func timeouts(plan []app.StopStep) map[string]time.Duration {
	got := make(map[string]time.Duration, len(plan))
	for _, step := range plan {
		got[step.Component] = step.Timeout
	}
	return got
}

func TestPlan_TimeoutsPinned(t *testing.T) {
	want := map[string]time.Duration{
		"event feed":             2 * time.Second,
		"acquisition scheduler":  35 * time.Second,
		"background tasks":       15 * time.Second,
		"leader election":        3 * time.Second,
		"detached request tasks": 10 * time.Second,
	}

	plan := app.PlanShutdown(app.StopComponents{}, app.StopBudgets{})
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
	if app.ServerDrainTimeout != 10*time.Second {
		t.Fatalf("server drain timeout = %v, want 10s", app.ServerDrainTimeout)
	}
	total := app.ServerDrainTimeout
	for _, timeout := range timeouts(app.PlanShutdown(app.StopComponents{}, app.StopBudgets{SchedulerSeconds: 600, BackgroundSeconds: 600, DetachedSeconds: 600})) {
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
		if got := timeouts(app.PlanShutdown(app.StopComponents{}, app.StopBudgets{SchedulerSeconds: budget}))["acquisition scheduler"]; got != want {
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
		plan := timeouts(app.PlanShutdown(app.StopComponents{}, app.StopBudgets{BackgroundSeconds: tc.background, DetachedSeconds: tc.detached}))
		if got := plan["background tasks"]; got != tc.wantBackground {
			t.Errorf("background %ds, detached %ds: background timeout %v, want %v", tc.background, tc.detached, got, tc.wantBackground)
		}
		if got := plan["detached request tasks"]; got != tc.wantDetached {
			t.Errorf("background %ds, detached %ds: detached timeout %v, want %v", tc.background, tc.detached, got, tc.wantDetached)
		}
	}
}

func withBackgroundBudget(plan []app.StopStep, budget time.Duration) []app.StopStep {
	for i := range plan {
		if plan[i].Component == app.StopBackground {
			plan[i].Timeout = budget
		}
	}
	return plan
}

func TestRun_ABackgroundDrainTimeoutKeepsTheLeaderLock(t *testing.T) {
	logged := captureLogsAt(t, slog.LevelInfo)
	election := &countingStopper{}
	release := make(chan struct{})
	defer close(release)
	blocked := func(context.Context) { <-release }

	outcomes := app.RunShutdown(context.Background(), withBackgroundBudget(app.PlanShutdown(app.StopComponents{
		Background:     blocked,
		LeaderElection: app.StopOf(election),
	}, app.StopBudgets{}), 20*time.Millisecond))

	if n := election.stops.Load(); n != 0 {
		t.Fatalf("election released %d time(s) although the background drain timed out", n)
	}
	if got := outcomeOf(t, outcomes, app.StopLeaderElection).Status; got != app.StopSkipped {
		t.Errorf("leader election outcome = %v, want skipped so the lock is retained", got)
	}
	if got := logged.String(); !strings.Contains(got, "level=ERROR") || !strings.Contains(got, "leadership intentionally NOT released") ||
		!strings.Contains(got, `component="leader election"`) || !strings.Contains(got, `requires="background tasks"`) {
		t.Errorf("missing error log for the retained lock: %q", got)
	}
}

func TestRun_ACleanBackgroundDrainReleasesTheLeaderLock(t *testing.T) {
	election := &countingStopper{}

	outcomes := app.RunShutdown(context.Background(), app.PlanShutdown(app.StopComponents{
		Background:     func(context.Context) {},
		LeaderElection: app.StopOf(election),
	}, app.StopBudgets{}))

	if n := election.stops.Load(); n != 1 {
		t.Fatalf("election released %d time(s) after a clean drain, want 1", n)
	}
	if got := outcomeOf(t, outcomes, app.StopLeaderElection).Status; got != app.StopCompleted {
		t.Errorf("leader election outcome = %v after a clean drain, want completed", got)
	}
}

func TestDrainServer_AHungHandlerIsClosedAfterTheDeadline(t *testing.T) {
	logged := captureLogsAt(t, slog.LevelDebug)
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
	go func() { app.DrainServer(context.Background(), srv.Config, 50*time.Millisecond); close(returned) }()
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
	logged := captureLogsAt(t, slog.LevelDebug)
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	app.DrainServer(context.Background(), srv.Config, time.Second)

	if got := logged.String(); got != "" {
		t.Errorf("idle drain logged %q, want nothing", got)
	}
	if resp, err := http.Get(srv.URL); err == nil {
		_ = resp.Body.Close()
		t.Error("server still answers after the drain")
	}
}

func lazyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://nobody@127.0.0.1:1/none?connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func redisClient(t *testing.T) *goredis.Client {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func poolClosed(pool *pgxpool.Pool) bool {
	err := pool.Ping(context.Background())
	return err != nil && strings.Contains(err.Error(), "closed pool")
}

func TestCloseDatastores_ACleanShutdownClosesBothWithoutWarning(t *testing.T) {
	logged := captureLogsAt(t, slog.LevelDebug)
	pool, client := lazyPool(t), redisClient(t)
	outcomes := []app.StopOutcome{
		{Component: app.StopBackground, Status: app.StopCompleted},
		{Component: app.StopLeaderElection, Status: app.StopCompleted},
	}
	app.CloseDatastores(context.Background(), outcomes, app.Datastores{Pool: pool, Redis: client})

	if !poolClosed(pool) {
		t.Error("pool still open after a clean shutdown")
	}
	if err := client.Ping(context.Background()).Err(); !errors.Is(err, goredis.ErrClosed) {
		t.Errorf("redis ping after close = %v, want %v", err, goredis.ErrClosed)
	}
	if got := logged.String(); got != "" {
		t.Errorf("clean shutdown logged %q, want nothing", got)
	}
}

func TestCloseDatastores_UnfinishedComponentsAreNamedInAWarning(t *testing.T) {
	logged := captureLogsAt(t, slog.LevelDebug)
	outcomes := []app.StopOutcome{
		{Component: app.StopEventFeed, Status: app.StopCompleted},
		{Component: app.StopScheduler, Status: app.StopTimedOut},
		{Component: app.StopLeaderElection, Status: app.StopSkipped},
	}
	app.CloseDatastores(context.Background(), outcomes, app.Datastores{})

	got := logged.String()
	if want := `level=WARN msg="closing DB/Redis while components are still shutting down" components="acquisition scheduler, leader election"`; !strings.Contains(got, want) {
		t.Errorf("log = %q, want %q", got, want)
	}
}

func TestCloseDatastores_OnlyASkippedLeaderElectionKeepsThePoolOpen(t *testing.T) {
	cases := map[string]struct {
		outcomes []app.StopOutcome
		keepOpen bool
	}{
		"skipped election":     {[]app.StopOutcome{{Component: app.StopLeaderElection, Status: app.StopSkipped}}, true},
		"timed out election":   {[]app.StopOutcome{{Component: app.StopLeaderElection, Status: app.StopTimedOut}}, false},
		"another skipped step": {[]app.StopOutcome{{Component: app.StopBackground, Status: app.StopSkipped}}, false},
		"no outcomes":          {nil, false},
		"completed election":   {[]app.StopOutcome{{Component: app.StopLeaderElection, Status: app.StopCompleted}}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			captureLogsAt(t, slog.LevelDebug)
			pool := lazyPool(t)
			app.CloseDatastores(context.Background(), tc.outcomes, app.Datastores{Pool: pool})

			if open := !poolClosed(pool); open != tc.keepOpen {
				t.Errorf("pool open = %v, want %v", open, tc.keepOpen)
			}
		})
	}
}

func TestCloseDatastores_ARetainedLeaderLockKeepsThePoolOpenButClosesRedis(t *testing.T) {
	logged := captureLogsAt(t, slog.LevelDebug)
	pool, client := lazyPool(t), redisClient(t)
	outcomes := []app.StopOutcome{
		{Component: app.StopBackground, Status: app.StopTimedOut},
		{Component: app.StopLeaderElection, Status: app.StopSkipped},
	}
	app.CloseDatastores(context.Background(), outcomes, app.Datastores{Pool: pool, Redis: client})

	if poolClosed(pool) {
		t.Error("pool closed although the leader lock is retained; closing it would release the lock before the process exits")
	}
	if err := client.Ping(context.Background()).Err(); !errors.Is(err, goredis.ErrClosed) {
		t.Errorf("redis ping after close = %v, want %v", err, goredis.ErrClosed)
	}
	if got := logged.String(); !strings.Contains(got, "leaving DB pool open") {
		t.Errorf("log = %q, want it to say the pool is left open", got)
	}
}

func TestCloseDatastores_ARedisCloseErrorIsLogged(t *testing.T) {
	logged := captureLogsAt(t, slog.LevelDebug)
	client := redisClient(t)
	if err := client.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	app.CloseDatastores(context.Background(), nil, app.Datastores{Redis: client})

	if got := logged.String(); !strings.Contains(got, "redis client close error") {
		t.Errorf("log = %q, want the redis close error", got)
	}
}

const allShutdownSteps = 5

func byComponent(outcomes []app.StopOutcome) map[string]app.StopOutcome {
	got := make(map[string]app.StopOutcome, len(outcomes))
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
	if got[app.StopDetachedTasks].Status != app.StopTimedOut {
		t.Errorf("%q = %v, want TimedOut: the held task is drained by the detached step", app.StopDetachedTasks, got[app.StopDetachedTasks].Status)
	}
	for _, name := range []string{app.StopEventFeed, app.StopScheduler, app.StopBackground, app.StopLeaderElection} {
		if got[name].Status != app.StopCompleted {
			t.Errorf("%q = %v, want Completed: it does not wait on detached tasks", name, got[name].Status)
		}
	}
	if b := got[app.StopDetachedTasks].Budget; b != 1*time.Second {
		t.Errorf("%q ran with budget %v, want DETACHED_DRAIN_BUDGET_SECONDS (1s)", app.StopDetachedTasks, b)
	}
	if b := got[app.StopBackground].Budget; b != 5*time.Second {
		t.Errorf("%q ran with budget %v, want BACKGROUND_DRAIN_BUDGET_SECONDS (5s)", app.StopBackground, b)
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
	if got[app.StopBackground].Status != app.StopTimedOut {
		t.Errorf("%q = %v, want TimedOut: the job loops were never cancelled", app.StopBackground, got[app.StopBackground].Status)
	}
	if got[app.StopLeaderElection].Status != app.StopSkipped {
		t.Errorf("%q = %v, want Skipped: the lock is kept while jobs may still run", app.StopLeaderElection, got[app.StopLeaderElection].Status)
	}
	if got[app.StopDetachedTasks].Status != app.StopCompleted {
		t.Errorf("%q = %v, want Completed: no detached task is held", app.StopDetachedTasks, got[app.StopDetachedTasks].Status)
	}
	if b := got[app.StopBackground].Budget; b != 1*time.Second {
		t.Errorf("%q ran with budget %v, want BACKGROUND_DRAIN_BUDGET_SECONDS (1s)", app.StopBackground, b)
	}
	if b := got[app.StopDetachedTasks].Budget; b != 5*time.Second {
		t.Errorf("%q ran with budget %v, want DETACHED_DRAIN_BUDGET_SECONDS (5s)", app.StopDetachedTasks, b)
	}
}
