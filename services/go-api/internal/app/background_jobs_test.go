package app

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/runloop"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type erasedRows struct {
	rows int64
	err  error
}

func (e erasedRows) EraseRowsOfDeletedIdentities(context.Context) (int64, error) {
	return e.rows, e.err
}

var errStoreDown = errors.New("connection refused")

func TestEraseDiscoveryRowsOfDeletedIdentities_TellsIdleApartFromFailed(t *testing.T) {
	cases := []struct {
		name    string
		erasers []discovery.DeletedIdentityEraser
		wantErr error
	}{
		{
			name: "an unreadable identity store idles",
			erasers: []discovery.DeletedIdentityEraser{
				erasedRows{err: fmt.Errorf("erase: %w", discovery.ErrIdentityStoreUnavailable)},
				erasedRows{rows: 7},
			},
			wantErr: nil,
		},
		{
			name: "any other failure is reported",
			erasers: []discovery.DeletedIdentityEraser{
				erasedRows{rows: 7},
				erasedRows{err: errStoreDown},
			},
			wantErr: errStoreDown,
		},
		{
			name: "a clean run reports success",
			erasers: []discovery.DeletedIdentityEraser{
				erasedRows{rows: 2},
				erasedRows{rows: 0},
			},
			wantErr: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := eraseDiscoveryRowsOfDeletedIdentities(context.Background(), tc.erasers)

			if !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want one satisfying %v", err, tc.wantErr)
			}
		})
	}
}

type countingEraser struct {
	calls *int
	erasedRows
}

func (c countingEraser) EraseRowsOfDeletedIdentities(ctx context.Context) (int64, error) {
	*c.calls++
	return c.erasedRows.EraseRowsOfDeletedIdentities(ctx)
}

func TestDeletedIdentitySweep_AFailingTableDoesNotSkipTheRest(t *testing.T) {
	var calls int
	errDiskFull := errors.New("disk full")
	erasers := []discovery.DeletedIdentityEraser{
		countingEraser{calls: &calls, erasedRows: erasedRows{err: errStoreDown}},
		countingEraser{calls: &calls, erasedRows: erasedRows{rows: 3}},
		countingEraser{calls: &calls, erasedRows: erasedRows{err: errDiskFull}},
	}

	err := eraseDiscoveryRowsOfDeletedIdentities(context.Background(), erasers)

	if calls != 3 {
		t.Errorf("erasers called = %d, want 3", calls)
	}
	if !errors.Is(err, errStoreDown) || !errors.Is(err, errDiskFull) {
		t.Errorf("error = %v, want both failures joined", err)
	}
}

func TestDeletedIdentitySweep_UnavailableStoreKeepsEarlierFailuresAndRunsTheRest(t *testing.T) {
	var calls int
	erasers := []discovery.DeletedIdentityEraser{
		countingEraser{calls: &calls, erasedRows: erasedRows{err: errStoreDown}},
		countingEraser{calls: &calls, erasedRows: erasedRows{err: discovery.ErrIdentityStoreUnavailable}},
		countingEraser{calls: &calls, erasedRows: erasedRows{rows: 4}},
	}

	err := eraseDiscoveryRowsOfDeletedIdentities(context.Background(), erasers)

	if !errors.Is(err, errStoreDown) {
		t.Errorf("error = %v, want the earlier failure kept", err)
	}
	if calls != 3 {
		t.Errorf("erasers called = %d, want 3", calls)
	}
}

func TestDeletedIdentitySweep_EveryEraserUnavailableIsAFailure(t *testing.T) {
	erasers := []discovery.DeletedIdentityEraser{
		erasedRows{err: discovery.ErrIdentityStoreUnavailable},
		erasedRows{err: discovery.ErrIdentityStoreUnavailable},
	}

	err := eraseDiscoveryRowsOfDeletedIdentities(context.Background(), erasers)

	if !errors.Is(err, discovery.ErrIdentityStoreUnavailable) {
		t.Errorf("error = %v, want a permanently idle sweep reported as a failure", err)
	}
}

type simpleJobLogCase struct {
	name         jobs.Name
	startedAttrs []any
	wantStarted  string
	wantFailed   string
}

func TestStartSimpleJob_LogsTheOldTextForEveryMigratedJob(t *testing.T) {
	cases := []simpleJobLogCase{
		{
			name:         jobs.OrphanedAudioReconcile,
			startedAttrs: []any{"interval", orphanedAudioReconcileInterval.String()},
			wantStarted:  `level=INFO msg="orphaned audio reconcile started" interval=` + orphanedAudioReconcileInterval.String() + "\n",
			wantFailed:   `level=WARN msg="orphaned audio reconcile failed" error=boom` + "\n",
		},
		{
			name:         jobs.DeletedIdentityErasure,
			startedAttrs: []any{"interval", deletedIdentityErasureInterval.String()},
			wantStarted:  `level=INFO msg="deleted identity erasure started" interval=` + deletedIdentityErasureInterval.String() + "\n",
			wantFailed:   `level=WARN msg="deleted identity erasure failed" error=boom` + "\n",
		},
		{
			name:         jobs.VocabularyRefresh,
			startedAttrs: nil,
			wantStarted:  `level=INFO msg="vocabulary refresh started"` + "\n",
			wantFailed:   `level=WARN msg="vocabulary refresh failed" error=boom` + "\n",
		},
	}

	for _, tc := range cases {
		t.Run(string(tc.name), func(t *testing.T) {
			logged := runFailingSimpleJob(t, tc)

			if !strings.Contains(logged, tc.wantStarted) {
				t.Errorf("started line drifted: want %q in\n%s", tc.wantStarted, logged)
			}
			if !strings.Contains(logged, tc.wantFailed) {
				t.Errorf("failure line drifted: want %q in\n%s", tc.wantFailed, logged)
			}
		})
	}
}

func TestStartSimpleJob_CountsAFailedRunAsAFailure(t *testing.T) {
	a := &App{}
	runFailingSimpleJobOn(t, a, simpleJobLogCase{name: jobs.OrphanedAudioReconcile})

	if failures := findJobHealth(t, a.jobs.Health(), string(jobs.OrphanedAudioReconcile)).Failures; failures == 0 {
		t.Error("a failing simple job did not count toward its failure signal")
	}
}

func runFailingSimpleJob(t *testing.T, tc simpleJobLogCase) string {
	t.Helper()
	return captureLogs(func() { runFailingSimpleJobOn(t, &App{}, tc) })
}

func runFailingSimpleJobOn(t *testing.T, a *App, tc simpleJobLogCase) {
	t.Helper()
	ran := make(chan struct{}, 1)
	startAwaitStop(t, a, ran, func(ctx context.Context) {
		a.startSimpleJob(tc.name, time.Millisecond, func(context.Context) error {
			signalRan(ran)
			return errors.New("boom")
		}, tc.startedAttrs...)
	})
}

func signalRan(ran chan struct{}) {
	select {
	case ran <- struct{}{}:
	default:
	}
}

func captureLogs(run func()) string {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(restore)
	run()
	return buf.String()
}

const jobRanDeadline = 2 * time.Second

type leaderElection struct{ passes atomic.Int64 }

func (*leaderElection) Start(context.Context) {}

func (*leaderElection) Await(context.Context) bool { return true }

func (e *leaderElection) LeaderContext(parent context.Context) (context.Context, context.CancelFunc, bool) {
	e.passes.Add(1)
	ctx, cancel := context.WithCancel(parent)
	return ctx, cancel, true
}

func (*leaderElection) Shutdown(context.Context) {}

type neverLeads struct{}

func (neverLeads) Start(context.Context) {}

func (neverLeads) Await(ctx context.Context) bool {
	<-ctx.Done()
	return false
}

func (neverLeads) LeaderContext(parent context.Context) (context.Context, context.CancelFunc, bool) {
	return parent, func() {}, false
}

func (neverLeads) Shutdown(context.Context) {}

const followerGrace = 50 * time.Millisecond

func jobRan(h runloop.JobHealth) bool {
	return h.Failures > 0 || h.Skipped > 0 || !h.LastSuccess.IsZero() || !h.LastFailure.IsZero()
}

func assertLeaderOnly(t *testing.T, name jobs.Name, build func(t *testing.T) *App) {
	t.Helper()
	follower := build(t)
	leadWith(t, follower, neverLeads{})
	time.Sleep(followerGrace)
	if h := findJobHealth(t, follower.jobs.Health(), string(name)); jobRan(h) {
		t.Errorf("job %q ran on an instance that is not the leader: %+v", name, h)
	}

	leader := build(t)
	leadWith(t, leader, &leaderElection{})
	waitForHealth(t, leader, name, jobRan)
}

func leadWith(t *testing.T, a *App, election jobs.Election) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		a.jobs.Background().Wait()
	})
	a.jobs.Lead(ctx, election)
}

func findJobHealth(t fataler, snapshot []runloop.JobHealth, name string) runloop.JobHealth {
	t.Helper()
	for _, h := range snapshot {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("job %q not found in health snapshot", name)
	return runloop.JobHealth{}
}

type fataler interface {
	Helper()
	Fatalf(format string, args ...any)
}

func startAwaitStop(t *testing.T, a *App, ran chan struct{}, register func(context.Context)) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		a.jobs.Background().Wait()
	}()
	register(ctx)
	a.jobs.Lead(ctx, &leaderElection{})
	awaitJobRan(t, a, ran, jobRanDeadline)
	awaitJobOutcomes(t, a, jobRanDeadline)
}

func awaitJobOutcomes(t fataler, a *App, deadline time.Duration) {
	t.Helper()
	giveUp := time.Now().Add(deadline)
	for _, name := range registeredJobNames(a) {
		for {
			h := findJobHealth(t, a.jobs.Health(), string(name))
			if !h.LastSuccess.IsZero() || !h.LastFailure.IsZero() {
				break
			}
			if time.Now().After(giveUp) {
				t.Fatalf("job %q ran but recorded no outcome within %s", name, deadline)
				return
			}
			time.Sleep(time.Millisecond)
		}
	}
}

func awaitJobRan(t fataler, a *App, ran chan struct{}, deadline time.Duration) {
	t.Helper()
	select {
	case <-ran:
	case <-time.After(deadline):
		t.Fatalf("job %v never ran within %s", registeredJobNames(a), deadline)
	}
}

type recordingFataler struct{ messages []string }

func (r *recordingFataler) Helper() {}

func (r *recordingFataler) Fatalf(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}

func TestAwaitJobRan_FailsNamingTheJobWhenItsBodyNeverRuns(t *testing.T) {
	a := &App{}
	a.startSimpleJob(jobs.VocabularyRefresh, time.Hour, func(context.Context) error { return nil })
	rec := &recordingFataler{}

	logged := captureLogs(func() { awaitJobRan(rec, a, make(chan struct{}), 10*time.Millisecond) })

	if len(rec.messages) != 1 || !strings.Contains(rec.messages[0], string(jobs.VocabularyRefresh)) {
		t.Errorf("want one failure naming %q, got %v", jobs.VocabularyRefresh, rec.messages)
	}
	if logged != "" {
		t.Errorf("unexpected log output %q", logged)
	}
}

func waitForHealth(t *testing.T, a *App, name jobs.Name, cond func(runloop.JobHealth) bool) runloop.JobHealth {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		h := findJobHealth(t, a.jobs.Health(), string(name))
		if cond(h) {
			return h
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %q stayed %+v", name, h)
		}
		time.Sleep(time.Millisecond)
	}
}

type failingLabelStore struct{ ran chan struct{} }

func (f failingLabelStore) BehavioralLabels(context.Context, time.Time) ([]discovery.BehavioralLabel, error) {
	signalRan(f.ran)
	return nil, errors.New("boom")
}

type failingPruner struct {
	ran                    chan struct{}
	discographyErr, evtErr error
}

func (f failingPruner) PruneDiscographyObserved(context.Context, time.Time) (int64, error) {
	signalRan(f.ran)
	return 0, f.discographyErr
}

func (f failingPruner) PruneEvents(context.Context, time.Time) (int64, error) {
	signalRan(f.ran)
	return 0, f.evtErr
}

func runStartedJob(t *testing.T, a *App, ran chan struct{}, register func(context.Context)) string {
	t.Helper()
	return captureLogs(func() { startAwaitStop(t, a, ran, register) })
}

func TestStartCorpusRefresh_LogsTheOldTextAndCountsAFailure(t *testing.T) {
	a := &App{cfg: &config.Config{BehavioralCorpusPath: "/tmp/corpus.json"}}
	ran := make(chan struct{}, 1)

	logged := runStartedJob(t, a, ran, func(ctx context.Context) {
		a.startCorpusRefresh(ctx, failingLabelStore{ran: ran})
	})

	for _, want := range []string{
		`level=INFO msg="behavioral corpus refresh started" path=/tmp/corpus.json` + "\n",
		`level=WARN msg="behavioral corpus materialize failed" error="build behavioral corpus: boom"` + "\n",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("want %q in\n%s", want, logged)
		}
	}
	if findJobHealth(t, a.jobs.Health(), string(jobs.BehavioralCorpusRefresh)).Failures == 0 {
		t.Error("a failing corpus refresh did not count toward its failure signal")
	}
}

func TestStartDiscographyPrune_LogsTheOldTextOnEachFailurePath(t *testing.T) {
	cases := []struct {
		name       string
		pruner     func(chan struct{}) failingPruner
		wantFailed string
		notWant    string
	}{
		{
			name: "discography prune fails",
			pruner: func(ran chan struct{}) failingPruner {
				return failingPruner{ran: ran, discographyErr: errors.New("boom")}
			},
			wantFailed: `level=WARN msg="discography event prune failed" error=boom` + "\n",
			notWant:    "discovery event retention prune failed",
		},
		{
			name:       "event prune fails",
			pruner:     func(ran chan struct{}) failingPruner { return failingPruner{ran: ran, evtErr: errors.New("boom")} },
			wantFailed: `level=WARN msg="discovery event retention prune failed" error=boom` + "\n",
			notWant:    "discography event prune failed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &App{}
			ran := make(chan struct{}, 1)

			logged := runStartedJob(t, a, ran, func(ctx context.Context) {
				a.startDiscographyPrune(ctx, tc.pruner(ran))
			})

			started := `level=INFO msg="discography event prune started" interval=` + discographyPruneInterval.String() + "\n"
			if !strings.Contains(logged, started) {
				t.Errorf("want %q in\n%s", started, logged)
			}
			if !strings.Contains(logged, tc.wantFailed) {
				t.Errorf("want %q in\n%s", tc.wantFailed, logged)
			}
			if strings.Contains(logged, tc.notWant) {
				t.Errorf("unexpected %q in\n%s", tc.notWant, logged)
			}
			if findJobHealth(t, a.jobs.Health(), string(jobs.DiscographyEventPrune)).Failures == 0 {
				t.Error("a failing prune did not count toward its failure signal")
			}
		})
	}
}

type stubAcquisitionPruner struct {
	ran    chan struct{}
	pruned int64
	err    error
}

func (s stubAcquisitionPruner) Prune(context.Context, time.Time) (int64, error) {
	signalRan(s.ran)
	return s.pruned, s.err
}

func TestStartAcquisitionPrune_LogsACountOnlyWhenRowsWereRemoved(t *testing.T) {
	const removedLog = `level=INFO msg="acquisition rows pruned" outcome_rows=3 rejection_rows=2` + "\n"
	cases := []struct {
		name       string
		outcomes   int64
		rejections int64
		wantLogged bool
	}{
		{name: "rows removed", outcomes: 3, rejections: 2, wantLogged: true},
		{name: "nothing removed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &App{}
			ran := make(chan struct{}, 2)
			logged := runStartedJob(t, a, ran, func(ctx context.Context) {
				a.startAcquisitionPrune(ctx,
					stubAcquisitionPruner{ran: ran, pruned: tc.outcomes},
					stubAcquisitionPruner{ran: ran, pruned: tc.rejections})
			})
			if got := strings.Contains(logged, removedLog); got != tc.wantLogged {
				t.Errorf("prune log present = %v, want %v in\n%s", got, tc.wantLogged, logged)
			}
		})
	}
}

func TestStartAcquisitionPrune_FailingPruneCountsTowardItsFailureSignal(t *testing.T) {
	a := &App{}
	ran := make(chan struct{}, 1)

	logged := runStartedJob(t, a, ran, func(ctx context.Context) {
		a.startAcquisitionPrune(ctx,
			stubAcquisitionPruner{ran: ran, err: errors.New("boom")},
			stubAcquisitionPruner{ran: ran})
	})

	want := `level=WARN msg="acquisition outcome prune failed" error=boom` + "\n"
	if !strings.Contains(logged, want) {
		t.Errorf("want %q in\n%s", want, logged)
	}
	if findJobHealth(t, a.jobs.Health(), string(jobs.AcquisitionPrune)).Failures == 0 {
		t.Error("a failing prune did not count toward its failure signal")
	}
}
