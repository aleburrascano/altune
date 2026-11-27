package jobs_test

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/platform/leader"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/polltest"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
)

const leaderLoopTick = time.Millisecond

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

func leading() *fakeElection {
	e := &fakeElection{}
	e.win()
	return e
}

func lead(t *testing.T, r *jobs.Runner, e jobs.Election) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		r.Wait()
	})
	r.Lead(ctx, e)
}

func captureLogs(t *testing.T) *logging.Capture {
	t.Helper()
	return logging.CaptureText(t, slog.LevelInfo)
}

func waitForAtLeast(t *testing.T, c *atomic.Int32, want int32) {
	t.Helper()
	if !polltest.Within(2*time.Second, func() bool { return c.Load() >= want }) {
		t.Fatalf("counter never reached %d (got %d)", want, c.Load())
	}
}

func findHealth(t *testing.T, r *jobs.Runner, name string) background.JobHealth {
	t.Helper()
	for _, h := range r.Health() {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("job %q not found in health snapshot", name)
	return background.JobHealth{}
}

func assertPassesFollowTheTerm(t *testing.T, e *fakeElection, passes *atomic.Int32, advance func(time.Duration)) {
	t.Helper()
	waitForAtLeast(t, passes, 3)

	e.lose()
	advance(30 * leaderLoopTick)
	stopped := passes.Load()

	advance(60 * leaderLoopTick)
	if extra := passes.Load() - stopped; extra > 1 {
		t.Fatalf("job made %d more passes after the term ended, want none", extra)
	}

	e.win()
	waitForAtLeast(t, passes, stopped+3)
}

func TestTicker_HealthRecordsSuccessAndFailure(t *testing.T) {
	r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
	var calls atomic.Int32
	r.Ticker("rollup", time.Millisecond, func(context.Context) error {
		if calls.Add(1)%2 == 0 {
			return errors.New("boom")
		}
		return nil
	})
	lead(t, &r, leading())

	waitForAtLeast(t, &calls, 4)

	h := findHealth(t, &r, "rollup")
	if !h.Enabled || h.LastSuccess.IsZero() || h.Failures == 0 || h.LastFailure.IsZero() {
		t.Errorf("health = %+v, want enabled with a last success, failures and a last failure", h)
	}
}

func TestTicker_RegistersTheJobBeforeLeadership(t *testing.T) {
	r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
	r.Ticker("rollup", time.Hour, func(context.Context) error { return nil })

	findHealth(t, &r, "rollup")
	if st, ok := r.SetEnabled("rollup", false); !ok || st.Name != "rollup" || st.Enabled {
		t.Fatalf("SetEnabled on a registered but not-yet-leading job = %+v, %v; want rollup disabled", st, ok)
	}
}

func TestSetEnabled_TheKillSwitchShowsInHealth(t *testing.T) {
	r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
	r.Control("paused")

	if _, ok := r.SetEnabled("paused", false); !ok {
		t.Fatal("SetEnabled on a registered job reported unknown")
	}
	if findHealth(t, &r, "paused").Enabled {
		t.Error("a disabled job must report Enabled=false in its health snapshot")
	}
}

func TestSetEnabled_AnUnknownJobRegistersNothing(t *testing.T) {
	r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}

	if _, ok := r.SetEnabled("typo", false); ok {
		t.Fatal("SetEnabled on an unregistered job reported ok")
	}
	if got := r.Health(); len(got) != 0 {
		t.Fatalf("unknown job name registered a job: %+v", got)
	}
}

func TestTicker_KillSwitchStopsAndResumesTheJob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
		var runs atomic.Int32
		r.Ticker("sweep", time.Millisecond, func(context.Context) error {
			runs.Add(1)
			return nil
		})
		lead(t, &r, leading())

		waitForAtLeast(t, &runs, 3)

		r.SetEnabled("sweep", false)
		time.Sleep(30 * time.Millisecond)
		baseline := runs.Load()
		time.Sleep(60 * time.Millisecond)
		if extra := runs.Load() - baseline; extra > 0 {
			t.Fatalf("disabled job kept running: %d extra runs", extra)
		}

		r.SetEnabled("sweep", true)
		waitForAtLeast(t, &runs, baseline+3)
	})
}

func TestEveryInstance_RunsWithoutLeadershipAndHonoursItsKillSwitch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
		lead(t, &r, &fakeElection{})
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)

		var runs atomic.Int32
		r.EveryInstance(ctx, jobs.BehavioralRankingRefresh, time.Millisecond, func(context.Context) error {
			runs.Add(1)
			return nil
		})

		waitForAtLeast(t, &runs, 3)
		if _, ok := r.SetEnabled(jobs.BehavioralRankingRefresh, false); !ok {
			t.Fatal("SetEnabled must find the behavioral ranking refresh job")
		}
		time.Sleep(30 * time.Millisecond)
		baseline := runs.Load()
		time.Sleep(30 * time.Millisecond)
		if extra := runs.Load() - baseline; extra > 0 {
			t.Fatalf("disabled job kept running: %d extra runs", extra)
		}
	})
}

func waitAsync(r *jobs.Runner) <-chan struct{} {
	waited := make(chan struct{})
	go func() {
		r.Wait()
		close(waited)
	}()
	return waited
}

func assertStillWaiting(t *testing.T, waited <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-waited:
		t.Fatalf("Wait returned while %s", what)
	case <-time.After(50 * time.Millisecond):
	}
}

func assertWaitReturns(t *testing.T, waited <-chan struct{}) {
	t.Helper()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return once everything had finished")
	}
}

func TestWait_BlocksWhileAJobRunIsInFlight(t *testing.T) {
	r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	r.EveryInstance(ctx, jobs.BehavioralRankingRefresh, time.Hour, func(context.Context) error {
		once.Do(func() { close(started) })
		<-release
		return nil
	})
	<-started
	cancel()
	waited := waitAsync(&r)

	assertStillWaiting(t, waited, "a job run was in flight")
	close(release)
	assertWaitReturns(t, waited)
}

func TestWait_BlocksWhileTheSharedBackgroundGroupIsRunning(t *testing.T) {
	r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
	release := make(chan struct{})
	r.Background().Track(func() { <-release })
	waited := waitAsync(&r)

	assertStillWaiting(t, waited, "the shared background group was running")
	close(release)
	assertWaitReturns(t, waited)
}

func TestTicker_RecoversFromAPanickingJob(t *testing.T) {
	logged := captureLogs(t)
	r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
	var calls atomic.Int32
	ran := make(chan struct{}, 8)
	r.Ticker("explode", time.Millisecond, func(context.Context) error {
		n := calls.Add(1)
		select {
		case ran <- struct{}{}:
		default:
		}
		if n == 1 {
			panic("boom")
		}
		return nil
	})
	lead(t, &r, leading())

	<-ran
	<-ran

	if got := logged.String(); !strings.Contains(got, "explode") || !strings.Contains(got, "boom") {
		t.Errorf("panic log = %q, want it to name the job and the panic value", got)
	}
	if failures := findHealth(t, &r, "explode").Failures; failures == 0 {
		t.Error("a recovered panic did not count toward the job's failure signal")
	}
}

func TestTicker_AWedgedRunIsCanceledAndTheTickerRecovers(t *testing.T) {
	r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
	var calls, successes atomic.Int32
	wedgedErr := make(chan error, 1)
	r.Ticker("wedged", 40*time.Millisecond, func(ctx context.Context) error {
		if calls.Add(1) == 1 {
			<-ctx.Done()
			wedgedErr <- context.Cause(ctx)
			return ctx.Err()
		}
		successes.Add(1)
		return nil
	})
	lead(t, &r, leading())

	waitForAtLeast(t, &successes, 1)

	if cause := <-wedgedErr; !errors.Is(cause, background.ErrRunBudgetExceeded) {
		t.Fatalf("wedged run canceled with cause %v, want runloop.ErrRunBudgetExceeded", cause)
	}
	if h := findHealth(t, &r, "wedged"); h.Failures < 1 || h.LastFailure.IsZero() || h.LastSuccess.IsZero() {
		t.Fatalf("health = %+v, want the wedged run as a failure and a later success", h)
	}
}

func TestWhenLeader_APanickingStartDoesNotStopTheNext(t *testing.T) {
	logged := captureLogs(t)
	r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
	survived := make(chan struct{})
	r.WhenLeader("panicky", func(context.Context) { panic("on-acquire boom") })
	r.WhenLeader("survivor", func(context.Context) { close(survived) })

	lead(t, &r, leading())

	select {
	case <-survived:
	case <-time.After(2 * time.Second):
		t.Fatal("a panic in one leader job prevented the next job from starting")
	}
	if got := logged.String(); !strings.Contains(got, "panicky") {
		t.Errorf("panic log did not name the job, got: %q", got)
	}
}

func TestLead_StartsTheElectionAndKeepsIt(t *testing.T) {
	r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
	if r.Election() != nil {
		t.Fatal("a runner that never led has an election")
	}
	e := leading()

	lead(t, &r, e)

	if r.Election() != e {
		t.Fatalf("Election() = %v, want the election Lead was given", r.Election())
	}
}

func TestLeaderContext_WithoutAnElectionEveryPassLeads(t *testing.T) {
	r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}

	ctx, release, ok := r.LeaderContext(context.Background())

	if !ok || ctx == nil {
		t.Fatal("a runner with no election refused the pass")
	}
	release()
}

func TestTicker_TheOldLeaderStopsAfterAHandoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		oldE, newE := leading(), &fakeElection{}
		oldRunner, newRunner := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}, jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
		var oldRuns, newRuns atomic.Int32
		oldRunner.Ticker("metrics rollup", leaderLoopTick, func(context.Context) error { oldRuns.Add(1); return nil })
		newRunner.Ticker("metrics rollup", leaderLoopTick, func(context.Context) error { newRuns.Add(1); return nil })
		lead(t, &oldRunner, oldE)

		waitForAtLeast(t, &oldRuns, 3)

		oldE.lose()
		newE.win()
		lead(t, &newRunner, newE)

		time.Sleep(30 * leaderLoopTick)
		baseline := oldRuns.Load()
		time.Sleep(60 * leaderLoopTick)
		if extra := oldRuns.Load() - baseline; extra > 1 {
			t.Fatalf("old leader kept running the job after handoff: %d extra runs", extra)
		}
		waitForAtLeast(t, &newRuns, 3)
	})
}

func TestTicker_AStaleLeadersWriteIsNotApplied(t *testing.T) {
	oldE, newE := leading(), &fakeElection{}
	var (
		mu      sync.Mutex
		applied []string
	)
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
	oldRunner, newRunner := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}, jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
	oldRunner.Ticker("metrics rollup", time.Hour, func(ctx context.Context) error {
		close(started)
		<-proceed
		err := write(ctx, "old")
		staleErr <- err
		return err
	})
	lead(t, &oldRunner, oldE)
	<-started

	oldE.lose()
	newE.win()
	newDone := make(chan struct{})
	newRunner.Ticker("metrics rollup", time.Hour, func(ctx context.Context) error {
		defer close(newDone)
		return write(ctx, "new")
	})
	lead(t, &newRunner, newE)
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

func TestTicker_RunsCanceledByLeadershipLossDoNotCountAsFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := leading()
		r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
		started := make(chan struct{}, 1)
		r.Ticker("interrupted", time.Hour, func(ctx context.Context) error {
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		})
		ctx, cancel := context.WithCancel(t.Context())
		r.Lead(ctx, e)

		for range 3 {
			<-started
			e.lose()
			synctest.Wait()
			e.win()
			time.Sleep(time.Hour)
		}
		<-started
		e.lose()
		synctest.Wait()

		h := findHealth(t, &r, "interrupted")
		if h.Failures != 0 || r.Control("interrupted").ConsecutiveFailures() != 0 || !h.LastFailure.IsZero() {
			t.Fatalf("leadership-lost runs counted as failures: %+v", h)
		}
		cancel()
		r.Wait()
	})
}

func TestTicker_TheAlertMonitorEvaluatesOnlyDuringItsTerm(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := leading()
		r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}

		var evaluations atomic.Int32
		monitor, err := monitoring.NewAlertMonitor(monitoring.NopNotifier{}, leaderLoopTick, time.Now, monitoring.Condition{
			Key: "dependency_down",
			Eval: func(context.Context) *monitoring.Alert {
				evaluations.Add(1)
				return nil
			},
		})
		if err != nil {
			t.Fatalf("NewAlertMonitor: %v", err)
		}
		r.Ticker(jobs.AlertMonitor, leaderLoopTick, monitor.Pass)
		lead(t, &r, e)

		assertPassesFollowTheTerm(t, e, &evaluations, time.Sleep)
	})
}

func TestTicker_TheEvalMeterRunsOnlyDuringItsTerm(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := leading()
		r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}

		var runs atomic.Int32
		meter, err := monitoring.NewEvalMeter(clock.Now, func(context.Context) (monitoring.EvalMeterResult, error) {
			runs.Add(1)
			return monitoring.EvalMeterResult{}, nil
		}, r.Control(jobs.EvalMeter).Disabled)
		if err != nil {
			t.Fatalf("NewEvalMeter: %v", err)
		}
		r.Ticker(jobs.EvalMeter, leaderLoopTick, meter.Pass)
		lead(t, &r, e)

		assertPassesFollowTheTerm(t, e, &runs, time.Sleep)
	})
}

func TestTicker_LosingTheTermCutsOffAnEvalMidRun(t *testing.T) {
	e := leading()
	r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}

	started := make(chan struct{})
	stoppedBecause := make(chan error, 1)
	meter, err := monitoring.NewEvalMeter(clock.Now, func(runCtx context.Context) (monitoring.EvalMeterResult, error) {
		close(started)
		<-runCtx.Done()
		stoppedBecause <- context.Cause(runCtx)
		return monitoring.EvalMeterResult{}, runCtx.Err()
	}, r.Control(jobs.EvalMeter).Disabled)
	if err != nil {
		t.Fatalf("NewEvalMeter: %v", err)
	}
	r.Ticker(jobs.EvalMeter, time.Hour, meter.Pass)
	lead(t, &r, e)
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

func TestSwitch_TheStreamRecoveryKillSwitchSuppressesReschedule(t *testing.T) {
	ctx := context.Background()
	r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
	user := ids.NewUserId(uuid.New())
	repo := librarytest.NewTrackRepo()
	store := librarytest.NewAudioStore()
	store.ErrOnStream = errors.New("not found")
	sched := &librarytest.Scheduler{}
	svc := library.NewStreamTrackService(repo, store, sched, &librarytest.Metrics{}, r.Switch(jobs.StreamRecovery), clock.Now)
	track, err := library.NewTrack(user, "Track", "Artist", "Album", time.Now, ids.Real)
	if err != nil {
		t.Fatal(err)
	}
	repo.Seed(track)
	if err := track.MarkReady("audio/gone.opus", ids.Real); err != nil {
		t.Fatal(err)
	}

	r.SetEnabled(jobs.StreamRecovery, false)

	if _, err := svc.Execute(ctx, user, track.ID); !errors.Is(err, library.ErrAudioTemporarilyUnavailable) {
		t.Fatalf("disabled recovery stream err = %v, want ErrAudioTemporarilyUnavailable", err)
	}
	if err := svc.RecoverIfMissing(ctx, user, track.ID); err != nil {
		t.Fatalf("disabled RecoverIfMissing err = %v, want nil no-op", err)
	}
	if len(sched.TrackIds) != 0 {
		t.Fatalf("disabled stream recovery scheduled %d re-acquisitions", len(sched.TrackIds))
	}
	if got, _ := repo.GetByID(ctx, track.ID, user); got == nil || got.DownloadStatus != library.AcquisitionReady {
		t.Fatalf("disabled stream recovery changed the track: %+v", got)
	}
	if h := findHealth(t, &r, string(jobs.StreamRecovery)); h.Enabled || h.Skipped != 2 {
		t.Fatalf("stream recovery health = %+v, want disabled with 2 skipped", h)
	}

	r.SetEnabled(jobs.StreamRecovery, true)

	if _, err := svc.Execute(ctx, user, track.ID); !errors.Is(err, library.ErrAudioNotAvailable) {
		t.Fatalf("re-enabled recovery stream err = %v, want ErrAudioNotAvailable", err)
	}
	if len(sched.TrackIds) != 1 {
		t.Fatalf("re-enabled stream recovery scheduled %d re-acquisitions, want 1", len(sched.TrackIds))
	}
}

type lockWonRecords chan slog.Record

func (c lockWonRecords) Enabled(context.Context, slog.Level) bool { return true }

func (c lockWonRecords) Handle(_ context.Context, r slog.Record) error {
	if r.Message == "leader.lock_won" {
		select {
		case c <- r:
		default:
		}
	}
	return nil
}

func (c lockWonRecords) WithAttrs([]slog.Attr) slog.Handler { return c }

func (c lockWonRecords) WithGroup(string) slog.Handler { return c }

func TestNewElection_WinsTheBackgroundLockAndAnnouncesATwentyFiveSecondSettle(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	won := make(lockWonRecords, 1)
	logging.UseHandler(t, won)

	r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
	election, err := jobs.NewElection(leader.AdaptLocks(database.NewAdvisoryLocks(pool).Acquire))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		election.Shutdown(context.Background())
		r.Wait()
	})
	r.Lead(ctx, election)

	select {
	case rec := <-won:
		settle := ""
		rec.Attrs(func(attr slog.Attr) bool {
			if attr.Key == "settle" {
				settle = attr.Value.String()
			}
			return true
		})
		if want := (25 * time.Second).String(); settle != want {
			t.Fatalf("leader.lock_won settle = %q, want %q", settle, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the background election never won its lock")
	}
}

type countedElection struct {
	*fakeElection
	counters leader.Counters
}

func (c countedElection) Counters() leader.Counters { return c.counters }

func TestLeaderCounters_ReportTheElectionsOwnCounters(t *testing.T) {
	r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
	if got := r.LeaderCounters(); got != (leader.Counters{}) {
		t.Errorf("before leading, LeaderCounters = %+v, want zero", got)
	}
	want := leader.Counters{Attempts: 4, Wins: 3, Failures: 7}

	lead(t, &r, countedElection{fakeElection: leading(), counters: want})

	if got := r.LeaderCounters(); got != want {
		t.Errorf("LeaderCounters = %+v, want the election's %+v", got, want)
	}
}

func TestLeaderCounters_AnElectionThatKeepsNoCountersReportsZero(t *testing.T) {
	r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}

	lead(t, &r, leading())

	if got := r.LeaderCounters(); got != (leader.Counters{}) {
		t.Errorf("LeaderCounters = %+v, want zero", got)
	}
}

func registrationPanic(register func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprint(r)
		}
	}()
	register()
	return ""
}

func TestTicker_RefusesAJobTheTableRunsOnEveryInstance(t *testing.T) {
	r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}

	msg := registrationPanic(func() {
		r.Ticker(jobs.BehavioralRankingRefresh, time.Hour, func(context.Context) error { return nil })
	})

	if !strings.Contains(msg, string(jobs.BehavioralRankingRefresh)) {
		t.Fatalf("Ticker(%q) panic = %q, want a refusal naming the job", jobs.BehavioralRankingRefresh, msg)
	}
}

func TestRunner_RefusesToRegisterAJobWithoutATicker(t *testing.T) {
	var r jobs.Runner

	tickerMsg := registrationPanic(func() {
		r.Ticker("rollup", time.Hour, func(context.Context) error { return nil })
	})
	everyMsg := registrationPanic(func() {
		r.EveryInstance(context.Background(), jobs.BehavioralRankingRefresh, time.Hour, func(context.Context) error { return nil })
	})

	noClock := jobs.Runner{NewTicker: clock.NewTicker}
	noClockMsg := registrationPanic(func() {
		noClock.Ticker("rollup", time.Hour, func(context.Context) error { return nil })
	})

	for name, msg := range map[string]string{"Ticker": tickerMsg, "EveryInstance": everyMsg, "Ticker without Now": noClockMsg} {
		if !strings.Contains(msg, "Now and NewTicker") {
			t.Errorf("%s without a ticker panic = %q, want a refusal naming NewTicker", name, msg)
		}
	}
}

func TestEveryInstance_RefusesEveryLeaderOnlyJob(t *testing.T) {
	leaderOnly := []jobs.Name{
		jobs.OrphanedAudioReconcile, jobs.DiscographyEventPrune, jobs.SpellingHintsRefresh,
		jobs.DeletedUserDataErasure, jobs.AcquisitionSourceCanary, jobs.StreamRecovery, jobs.AcquisitionPrune,
		jobs.AcquisitionUpgrade, jobs.UnreleasedRefresh, jobs.EvalMeter, jobs.AlertMonitor,
	}
	for _, name := range leaderOnly {
		t.Run(string(name), func(t *testing.T) {
			r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
			ctx, cancel := context.WithCancel(context.Background())
			defer func() {
				cancel()
				r.Wait()
			}()

			msg := registrationPanic(func() {
				r.EveryInstance(ctx, name, time.Hour, func(context.Context) error { return nil })
			})

			if !strings.Contains(msg, string(name)) {
				t.Fatalf("EveryInstance(%q) panic = %q, want a refusal naming the leader-only job", name, msg)
			}
		})
	}
}

func TestStartTicker_LogsThatTheJobStartedWithItsAttributes(t *testing.T) {
	cases := []struct {
		name  jobs.Name
		attrs []any
		want  string
	}{
		{jobs.OrphanedAudioReconcile, []any{"interval", "10m0s"}, `level=INFO msg="orphaned audio reconcile started" interval=10m0s` + "\n"},
		{jobs.SpellingHintsRefresh, nil, `level=INFO msg="vocabulary refresh started"` + "\n"},
	}
	for _, tc := range cases {
		t.Run(string(tc.name), func(t *testing.T) {
			logs := captureLogs(t)
			r := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}

			r.StartTicker(context.Background(), tc.name, time.Hour, func(context.Context) error { return nil }, tc.attrs...)

			if !strings.Contains(logs.String(), tc.want) {
				t.Errorf("want %q in\n%s", tc.want, logs.String())
			}
			findHealth(t, &r, string(tc.name))
		})
	}
}

func TestWarnOnFailure_LogsTheFailureAndReturnsIt(t *testing.T) {
	logs := captureLogs(t)
	boom := errors.New("boom")

	err := jobs.WarnOnFailure(jobs.SpellingHintsRefresh, func(context.Context) error { return boom })(context.Background())

	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want the run's failure so the runner counts it", err)
	}
	if want := `level=WARN msg="vocabulary refresh failed" error=boom` + "\n"; !strings.Contains(logs.String(), want) {
		t.Errorf("want %q in\n%s", want, logs.String())
	}
}

func TestWarnOnFailure_ASuccessLogsNothing(t *testing.T) {
	logs := captureLogs(t)

	err := jobs.WarnOnFailure(jobs.SpellingHintsRefresh, func(context.Context) error { return nil })(context.Background())

	if err != nil || logs.Len() != 0 {
		t.Errorf("err = %v, logs = %q, want nil and nothing logged", err, logs.String())
	}
}
