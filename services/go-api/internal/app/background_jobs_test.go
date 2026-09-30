package app

import (
	providermetrics "altune/go-api/internal/discovery/adapters/providermetrics"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/config"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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
		erasers []discoveryPorts.DeletedIdentityEraser
		wantErr error
	}{
		{
			name: "an unreadable identity store idles",
			erasers: []discoveryPorts.DeletedIdentityEraser{
				erasedRows{err: fmt.Errorf("erase: %w", discoveryPorts.ErrIdentityStoreUnavailable)},
				erasedRows{rows: 7},
			},
			wantErr: nil,
		},
		{
			name: "any other failure is reported",
			erasers: []discoveryPorts.DeletedIdentityEraser{
				erasedRows{rows: 7},
				erasedRows{err: errStoreDown},
			},
			wantErr: errStoreDown,
		},
		{
			name: "a clean run reports success",
			erasers: []discoveryPorts.DeletedIdentityEraser{
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
	erasers := []discoveryPorts.DeletedIdentityEraser{
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
	erasers := []discoveryPorts.DeletedIdentityEraser{
		countingEraser{calls: &calls, erasedRows: erasedRows{err: errStoreDown}},
		countingEraser{calls: &calls, erasedRows: erasedRows{err: discoveryPorts.ErrIdentityStoreUnavailable}},
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
	erasers := []discoveryPorts.DeletedIdentityEraser{
		erasedRows{err: discoveryPorts.ErrIdentityStoreUnavailable},
		erasedRows{err: discoveryPorts.ErrIdentityStoreUnavailable},
	}

	err := eraseDiscoveryRowsOfDeletedIdentities(context.Background(), erasers)

	if !errors.Is(err, discoveryPorts.ErrIdentityStoreUnavailable) {
		t.Errorf("error = %v, want a permanently idle sweep reported as a failure", err)
	}
}

type simpleJobLogCase struct {
	name         jobName
	startedAttrs []any
	wantStarted  string
	wantFailed   string
}

func TestStartSimpleJob_LogsTheOldTextForEveryMigratedJob(t *testing.T) {
	cases := []simpleJobLogCase{
		{
			name:         jobOrphanedAudioReconcile,
			startedAttrs: []any{"interval", orphanedAudioReconcileInterval.String()},
			wantStarted:  `level=INFO msg="orphaned audio reconcile started" interval=` + orphanedAudioReconcileInterval.String() + "\n",
			wantFailed:   `level=WARN msg="orphaned audio reconcile failed" error=boom` + "\n",
		},
		{
			name:         jobDeletedIdentityErasure,
			startedAttrs: []any{"interval", deletedIdentityErasureInterval.String()},
			wantStarted:  `level=INFO msg="deleted identity erasure started" interval=` + deletedIdentityErasureInterval.String() + "\n",
			wantFailed:   `level=WARN msg="deleted identity erasure failed" error=boom` + "\n",
		},
		{
			name:         jobVocabularyRefresh,
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
	runFailingSimpleJobOn(t, a, simpleJobLogCase{name: jobOrphanedAudioReconcile})

	if failures := a.job(jobOrphanedAudioReconcile).failures.Load(); failures == 0 {
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

type fataler interface {
	Helper()
	Fatalf(format string, args ...any)
}

func startAwaitStop(t *testing.T, a *App, ran chan struct{}, register func(context.Context)) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		a.wg.Wait()
	}()
	register(ctx)
	for _, job := range a.backgroundStarts {
		job.start(ctx)
	}
	awaitJobRan(t, a, ran, jobRanDeadline)
	awaitJobOutcomes(t, a, jobRanDeadline)
}

func awaitJobOutcomes(t fataler, a *App, deadline time.Duration) {
	t.Helper()
	giveUp := time.Now().Add(deadline)
	for _, job := range a.backgroundStarts {
		jc := a.job(job.name)
		for jc.lastSuccess.Load() == 0 && jc.lastFailure.Load() == 0 {
			if time.Now().After(giveUp) {
				t.Fatalf("job %q ran but recorded no outcome within %s", job.name, deadline)
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
		names := make([]string, 0, len(a.backgroundStarts))
		for _, job := range a.backgroundStarts {
			names = append(names, string(job.name))
		}
		t.Fatalf("job %v never ran within %s", names, deadline)
	}
}

type recordingFataler struct{ messages []string }

func (r *recordingFataler) Helper() {}

func (r *recordingFataler) Fatalf(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}

func TestAwaitJobRan_FailsNamingTheJobWhenItsBodyNeverRuns(t *testing.T) {
	a := &App{}
	a.startSimpleJob(jobVocabularyRefresh, time.Hour, func(context.Context) error { return nil })
	rec := &recordingFataler{}

	logged := captureLogs(func() { awaitJobRan(rec, a, make(chan struct{}), 10*time.Millisecond) })

	if len(rec.messages) != 1 || !strings.Contains(rec.messages[0], string(jobVocabularyRefresh)) {
		t.Errorf("want one failure naming %q, got %v", jobVocabularyRefresh, rec.messages)
	}
	if logged != "" {
		t.Errorf("unexpected log output %q", logged)
	}
}

func flipNamedJob(t *testing.T, a *App, name jobName, action string) {
	t.Helper()
	if _, ok := a.SetJobEnabled(name, action == "enable"); !ok {
		t.Fatalf("SetJobEnabled(%q, %s) reported unknown job", name, action)
	}
}

func waitForHealth(t *testing.T, a *App, name jobName, cond func(JobHealth) bool) JobHealth {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		h := findJobHealth(t, a.JobHealth(), string(name))
		if cond(h) {
			return h
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %q stayed %+v", name, h)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBackgroundChartCallsAreCounted(t *testing.T) {
	rt := &countingProviderRT{}
	base := newClientFactory(countedProviderTransport(rt))
	a := &App{cfg: &config.Config{}}

	charts := a.buildChartProviders(base)
	if len(charts) == 0 {
		t.Fatal("precondition: the Deezer chart provider must be wired")
	}
	before := providermetrics.ReadSnapshot()
	if _, err := charts[0].FetchCharts(context.Background(), 1); err != nil {
		t.Fatalf("fetch charts: %v", err)
	}
	counted := totalProviderCounts(providermetrics.ReadSnapshot()) - totalProviderCounts(before)

	if rt.roundTrips() == 0 {
		t.Fatal("the chart fetch made no provider call, so it proves nothing about the transport")
	}
	if counted != int64(rt.roundTrips()) {
		t.Errorf("provider counters moved by %d over %d chart calls, want one count per call", counted, rt.roundTrips())
	}
}

type failingLabelStore struct{ ran chan struct{} }

func (f failingLabelStore) BehavioralLabels(context.Context, time.Time) ([]discoveryPorts.BehavioralLabel, error) {
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
	if a.job(jobBehavioralCorpusRefresh).failures.Load() == 0 {
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
			if a.job(jobDiscographyEventPrune).failures.Load() == 0 {
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
	if a.job(jobAcquisitionPrune).failures.Load() == 0 {
		t.Error("a failing prune did not count toward its failure signal")
	}
}
