package app

import (
	adminAlert "altune/go-api/internal/observe/alert"
	"altune/go-api/internal/observe/evalmeter"
	"altune/go-api/internal/shared/leader"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartEveryInstanceTicker_RunsWithoutLeadership(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var runs atomic.Int32
	a := &App{election: &fakeElection{}}
	a.startEveryInstanceTicker(ctx, jobBehavioralRankingRefresh, time.Millisecond, func(context.Context) error {
		runs.Add(1)
		return nil
	})

	waitForAtLeast(t, &runs, 3)
	if _, ok := a.SetJobEnabled(jobBehavioralRankingRefresh, false); !ok {
		t.Fatal("SetJobEnabled must find the behavioral ranking refresh job")
	}
	time.Sleep(30 * time.Millisecond)
	baseline := runs.Load()
	time.Sleep(30 * time.Millisecond)
	if extra := runs.Load() - baseline; extra > 0 {
		t.Fatalf("disabled job kept running: %d extra runs", extra)
	}
}

// fakeElection stands in for leader.Election so a handoff can be simulated
// without a live Postgres advisory lock. win/lose flip leadership to model the
// window where one instance loses the lock and another acquires it; like the
// real election, lose cancels every job context issued during the term.
type fakeElection struct {
	mu      sync.Mutex
	leader  bool
	cancels []context.CancelCauseFunc
}

func (f *fakeElection) Start(context.Context) {}

func (f *fakeElection) Await(context.Context) bool { return true }

func (f *fakeElection) LeaderContext(parent context.Context) (context.Context, context.CancelFunc, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.leader {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancelCause(parent)
	f.cancels = append(f.cancels, cancel)
	return ctx, func() { cancel(context.Canceled) }, true
}

func (f *fakeElection) win() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leader = true
}

func (f *fakeElection) lose() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leader = false
	for _, cancel := range f.cancels {
		cancel(leader.ErrLeadershipLost)
	}
	f.cancels = nil
}

func (f *fakeElection) Shutdown(context.Context) { f.lose() }

// waitForAtLeast polls an atomic counter until it reaches want or the deadline
// elapses, so the test can prove the job actually ran before the handoff
// instead of racing it.
func waitForAtLeast(t *testing.T, c *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.Load() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("counter never reached %d (got %d)", want, c.Load())
}

// leaderLoopTick is the interval the self-looping leader-only jobs (the alert
// monitor, the eval meter) run at in these tests, short enough that "stopped
// within one interval" is observable in milliseconds.
const leaderLoopTick = time.Millisecond

// assertPassesFollowTheTerm drives one self-looping leader-only job through a
// lost and a regained leadership term: its passes must stop once the term ends
// and resume once a new one begins.
func assertPassesFollowTheTerm(t *testing.T, e *fakeElection, passes *atomic.Int32) {
	t.Helper()
	waitForAtLeast(t, passes, 3)

	e.lose()
	time.Sleep(30 * leaderLoopTick) // let a pass already in flight drain
	stopped := passes.Load()

	time.Sleep(60 * leaderLoopTick)
	if extra := passes.Load() - stopped; extra > 1 {
		t.Fatalf("job made %d more passes after the term ended, want none", extra)
	}

	e.win()
	waitForAtLeast(t, passes, stopped+3)
}

// TestAlertMonitor_LeadershipHandoff_EvaluatesOnlyDuringItsTerm is the
// regression for #2014: the monitor owns its own tick loop, started once on the
// first leadership win with the app-lifetime context, so a leader whose DB
// session died kept evaluating conditions while the instance that took over
// evaluated the same ones — and every firing condition logged twice.
func TestAlertMonitor_LeadershipHandoff_EvaluatesOnlyDuringItsTerm(t *testing.T) {
	e := &fakeElection{}
	e.win()
	a := &App{election: e}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var evaluations atomic.Int32
	monitor := adminAlert.NewMonitor(adminAlert.NopNotifier{}, leaderLoopTick, adminAlert.Condition{
		Key: "dependency_down",
		Eval: func(context.Context) *adminAlert.Alert {
			evaluations.Add(1)
			return nil
		},
	}).WithLeadership(a.leaderContext)
	monitor.Start(ctx)

	assertPassesFollowTheTerm(t, e, &evaluations)
}

// TestEvalMeter_LeadershipHandoff_RunsOnlyDuringItsTerm is the eval-runner half
// of #2014: two instances running the smoke eval at once doubles its cost for
// one usable scorecard, so the meter's runs must belong to the term too.
func TestEvalMeter_LeadershipHandoff_RunsOnlyDuringItsTerm(t *testing.T) {
	e := &fakeElection{}
	e.win()
	a := &App{election: e}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var runs atomic.Int32
	meter := evalmeter.New(true, leaderLoopTick, func(context.Context) (evalmeter.Result, error) {
		runs.Add(1)
		return evalmeter.Result{}, nil
	}).WithLeadership(a.leaderContext)
	meter.Start(ctx)

	assertPassesFollowTheTerm(t, e, &runs)
}

// TestEvalMeter_LeadershipLostMidRun_RunIsCutOff covers the other half of the
// eval meter's exposure in #2014: a single eval may run for minutes, so gating
// it at the start of a run is not enough — one that is still going when the
// session dies must be canceled, not left racing the successor's run.
func TestEvalMeter_LeadershipLostMidRun_RunIsCutOff(t *testing.T) {
	e := &fakeElection{}
	e.win()
	a := &App{election: e}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	stoppedBecause := make(chan error, 1)
	meter := evalmeter.New(true, time.Hour, func(runCtx context.Context) (evalmeter.Result, error) {
		close(started)
		<-runCtx.Done()
		stoppedBecause <- context.Cause(runCtx)
		return evalmeter.Result{}, runCtx.Err()
	}).WithLeadership(a.leaderContext)
	meter.Start(ctx)
	<-started

	e.lose()

	select {
	case err := <-stoppedBecause:
		if !errors.Is(err, leader.ErrLeadershipLost) {
			t.Fatalf("in-flight eval ended with %v, want it canceled with ErrLeadershipLost", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight eval was still running 2s after the term ended, want it canceled")
	}
}

// TestRunTicker_LeaderHandoff_OldLeaderStopsRunning is the regression for #391:
// a leader-gated ticker must re-check leadership on every tick so that, when a
// blue-green deploy hands the lock to a new instance, the old instance stops
// running the job. Before the fix runTicker fired forever once leadership was
// first won, so the old and new leaders ran the same job concurrently.
func TestRunTicker_LeaderHandoff_OldLeaderStopsRunning(t *testing.T) {
	const tick = time.Millisecond

	oldE := &fakeElection{}
	oldE.win()
	newE := &fakeElection{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oldApp := &App{election: oldE}
	newApp := &App{election: newE}

	var oldRuns, newRuns atomic.Int32
	oldApp.runTicker(ctx, "metrics rollup", tick, func(context.Context) error { oldRuns.Add(1); return nil })

	// The old instance is the sole leader: it must be running the job.
	waitForAtLeast(t, &oldRuns, 3)

	// Blue-green handoff: the old instance loses the lock, the new one wins it
	// and starts its own copy of the same job.
	oldE.lose()
	newE.win()
	newApp.runTicker(ctx, "metrics rollup", tick, func(context.Context) error { newRuns.Add(1); return nil })

	// Let any in-flight tick on the old instance drain.
	time.Sleep(30 * tick)
	baseline := oldRuns.Load()

	// Across many further ticks the old instance must not run the job again.
	time.Sleep(60 * tick)
	extra := oldRuns.Load() - baseline
	if extra > 1 {
		t.Fatalf("old leader kept running the job after handoff: %d extra runs", extra)
	}

	// Sanity: the new leader actually picked the job up.
	waitForAtLeast(t, &newRuns, 3)
}

// TestRunTicker_LeadershipLostMidTick_StaleWriteNotApplied is the regression
// for #1016: leadership used to be re-checked only at the start of a tick, so a
// leader whose DB session died while fn was still running carried on and
// committed its write concurrently with the successor's run of the same job.
// The in-flight run's context must be canceled on the loss, so its write is
// refused and only the new leader's lands.
func TestRunTicker_LeadershipLostMidTick_StaleWriteNotApplied(t *testing.T) {
	oldE := &fakeElection{}
	oldE.win()
	newE := &fakeElection{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu      sync.Mutex
		applied []string
	)
	// write models a leader-only job's DB write: like a pgx Exec, it is refused
	// once the context it runs under is done.
	write := func(ctx context.Context, who string) error {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		mu.Lock()
		defer mu.Unlock()
		applied = append(applied, who)
		return nil
	}

	started := make(chan struct{})
	proceed := make(chan struct{})
	staleErr := make(chan error, 1)
	oldApp := &App{election: oldE}
	oldApp.runTicker(ctx, "metrics rollup", time.Hour, func(ctx context.Context) error {
		close(started)
		<-proceed // the job body is still running when the session dies
		err := write(ctx, "old")
		staleErr <- err
		return err
	})
	<-started

	// The old leader's session dies mid-tick; the new leader wins and runs the job.
	oldE.lose()
	newE.win()
	newApp := &App{election: newE}
	newDone := make(chan struct{})
	newApp.runTicker(ctx, "metrics rollup", time.Hour, func(ctx context.Context) error {
		defer close(newDone)
		return write(ctx, "new")
	})
	<-newDone

	close(proceed)
	if err := <-staleErr; !errors.Is(err, leader.ErrLeadershipLost) {
		t.Fatalf("stale leader's write returned %v, want it refused with ErrLeadershipLost", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(applied) != 1 || applied[0] != "new" {
		t.Fatalf("applied writes = %v, want only the new leader's [new]", applied)
	}
}

// TestRunTicker_RecoversFromPanickingJob is the regression for #392: scheduled
// jobs run in their own goroutines outside any HTTP request, so the
// httputil.Recoverer that guards request handlers cannot catch them. Without a
// recover() of its own, a single panicking tick terminates the whole process
// and takes down live traffic. The tick must be contained: the panic is
// logged (naming the job) and the loop survives to tick again.
func TestRunTicker_RecoversFromPanickingJob(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})))
	defer slog.SetDefault(restore)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	ran := make(chan struct{}, 8)
	a := &App{}
	a.runTicker(ctx, "explode", time.Millisecond, func(context.Context) error {
		n := calls.Add(1)
		ran <- struct{}{}
		if n == 1 {
			panic("boom")
		}
		return nil
	})

	<-ran // first (immediate) invocation panics; without recover() this kills the process
	<-ran // a second tick proves the goroutine survived the panic
	cancel()

	logged := buf.String()
	if !strings.Contains(logged, "explode") {
		t.Errorf("panic log did not name the job, got: %q", logged)
	}
	if !strings.Contains(logged, "boom") {
		t.Errorf("panic log did not include the panic value, got: %q", logged)
	}

	// A contained panic must still register as a failed run in the health signal
	// rather than vanishing silently.
	if failures := a.job("explode").failures.Load(); failures == 0 {
		t.Error("a recovered panic did not count toward the job's failure signal")
	}
}

// TestWhenLeaderJobs_RecoverOnAcquire guards the leader-acquire path: a job
// that panics the moment leadership is acquired must not escape the shared
// goroutine and crash the process, and must not prevent the jobs registered
// after it from starting.
func TestWhenLeaderJobs_RecoverOnAcquire(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})))
	defer slog.SetDefault(restore)

	a := &App{}
	var secondRan atomic.Bool
	a.whenLeader("panicky", func(context.Context) { panic("on-acquire boom") })
	a.whenLeader("survivor", func(context.Context) { secondRan.Store(true) })

	for _, job := range a.backgroundStarts {
		guard(job.name, func() { job.start(context.Background()) })
	}

	if !secondRan.Load() {
		t.Fatal("a panic in one leader job prevented the next job from starting")
	}
	logged := buf.String()
	if !strings.Contains(logged, "panicky") {
		t.Errorf("panic log did not name the job, got: %q", logged)
	}
}

// TestRunTicker_WedgedRunIsCanceledAndTickerRecovers is the regression for
// #1017: the ticker loop calls each job synchronously, so a run blocked on a
// dependency call that never returns used to wedge that job forever. Each run
// must now be bounded by a per-invocation budget: the wedged run is canceled,
// counted as a failure, and the next tick fires and succeeds.
func TestRunTicker_WedgedRunIsCanceledAndTickerRecovers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls, successes atomic.Int32
	wedgedErr := make(chan error, 1)
	a := &App{}
	a.runTicker(ctx, "wedged", 40*time.Millisecond, func(ctx context.Context) error {
		if calls.Add(1) == 1 {
			// Models a hung DB call: it returns only once its context is done.
			<-ctx.Done()
			wedgedErr <- context.Cause(ctx)
			return ctx.Err()
		}
		successes.Add(1)
		return nil
	})

	waitForAtLeast(t, &successes, 1)

	if cause := <-wedgedErr; !errors.Is(cause, errJobRunBudgetExceeded) {
		t.Fatalf("wedged run canceled with cause %v, want errJobRunBudgetExceeded", cause)
	}
	h := findJobHealth(t, a.JobHealth(), "wedged")
	if h.Failures < 1 || h.LastFailure.IsZero() {
		t.Fatalf("wedged run not recorded as a failure: %+v", h)
	}
	if h.LastSuccess.IsZero() {
		t.Fatalf("ticker did not recover after the wedged run: %+v", h)
	}
}

// TestJobRunBudget_IsFractionOfInterval pins the budget below the interval so a
// canceled run always frees the loop before the next tick is due.
func TestJobRunBudget_IsFractionOfInterval(t *testing.T) {
	for _, interval := range []time.Duration{10 * time.Minute, 6 * time.Hour, 24 * time.Hour} {
		if got := jobRunBudget(interval); got <= 0 || got >= interval {
			t.Errorf("jobRunBudget(%v) = %v, want within (0, interval)", interval, got)
		}
	}
}

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
