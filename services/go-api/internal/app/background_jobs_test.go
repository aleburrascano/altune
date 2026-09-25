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
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// erasedRows is a discovery table that erases a fixed number of rows, or fails
// the way the sweep has to tell apart.
type erasedRows struct {
	rows int64
	err  error
}

func (e erasedRows) EraseRowsOfDeletedIdentities(context.Context) (int64, error) {
	return e.rows, e.err
}

var errStoreDown = errors.New("connection refused")

// TestEraseDiscoveryRowsOfDeletedIdentities_TellsIdleApartFromFailed holds the
// two answers the sweep must not confuse. An identity store this deployment
// cannot read is idle: it erases nothing and reports success, because "no
// identity is visible" must never be acted on as "every identity was deleted",
// and an hourly job that failed on every plain-Postgres deployment would be
// noise nobody reads. Any other failure is reported, so the job's health signal
// degrades and the erasure is retried rather than counted as done.
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

// simpleJobLogCase is one migrated job's two log lines, spelled here as the
// literal text they carried before startSimpleJob existed.
type simpleJobLogCase struct {
	name         jobName
	startedAttrs []any
	wantStarted  string
	wantFailed   string
}

// TestStartSimpleJob_LogsTheOldTextForEveryMigratedJob is the guard for #2010:
// the five hand-typed start/warn blocks collapsed into one helper, and alerting
// keys on their exact text, so each line is pinned whole — level, message and
// attributes — and a job that gains or loses an attribute fails here.
func TestStartSimpleJob_LogsTheOldTextForEveryMigratedJob(t *testing.T) {
	cases := []simpleJobLogCase{
		{
			name:         jobStalePendingReconcile,
			startedAttrs: []any{"interval", stalePendingReconcileInterval.String()},
			wantStarted:  `level=INFO msg="stale pending reconcile started" interval=` + stalePendingReconcileInterval.String() + "\n",
			wantFailed:   `level=WARN msg="stale pending reconcile failed" error=boom` + "\n",
		},
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

// TestStartSimpleJob_CountsAFailedRunAsAFailure holds the other half of the
// warn block the helper absorbed: the run's error is still returned to the
// ticker, so a failing job degrades its health signal instead of only logging.
func TestStartSimpleJob_CountsAFailedRunAsAFailure(t *testing.T) {
	a := &App{}
	runFailingSimpleJobOn(t, a, simpleJobLogCase{name: jobStalePendingReconcile})

	if failures := a.job(jobStalePendingReconcile).failures.Load(); failures == 0 {
		t.Error("a failing simple job did not count toward its failure signal")
	}
}

// runFailingSimpleJob schedules tc as a job that always fails, lets it tick
// once, and returns everything it logged.
func runFailingSimpleJob(t *testing.T, tc simpleJobLogCase) string {
	t.Helper()
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(restore)

	runFailingSimpleJobOn(t, &App{}, tc)
	return buf.String()
}

// runFailingSimpleJobOn drives one tick of tc on a, leadership included, and
// returns once the job's goroutine has drained so its output can be read.
func runFailingSimpleJobOn(t *testing.T, a *App, tc simpleJobLogCase) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ran := make(chan struct{}, 1)
	a.startSimpleJob(ctx, tc.name, time.Millisecond, func(context.Context) error {
		select {
		case ran <- struct{}{}:
		default:
		}
		return errors.New("boom")
	}, tc.startedAttrs...)

	for _, job := range a.backgroundStarts {
		job.start(ctx)
	}
	<-ran
	cancel()
	a.wg.Wait()
}

// flipNamedJob flips the kill switch of the job with the given wire name
// through the operator-gated admin router.
func flipNamedJob(t *testing.T, srv http.Handler, name jobName, action string) {
	t.Helper()
	path := "/admin/jobs/" + url.PathEscape(string(name)) + "/" + action
	if code, body := callAdmin(t, srv, http.MethodPost, path, operatorToken); code != http.StatusOK {
		t.Fatalf("POST %s = %d, want 200; body %s", path, code, body)
	}
}

type countingStalePendingFailer struct{ calls atomic.Int64 }

func (f *countingStalePendingFailer) FailStalePending(context.Context, time.Time, string) (int, error) {
	f.calls.Add(1)
	return 0, nil
}

// TestStalePendingReconcile_AdminKillSwitchSuppressesSweep is the regression for
// #1062 on the reconcile half: the production stale-pending job, disabled through
// the admin router, must not touch the repository on its leader-acquired run.
func TestStalePendingReconcile_AdminKillSwitchSuppressesSweep(t *testing.T) {
	a := &App{}
	srv := jobsAdminServer(t, a, true)
	repo := &countingStalePendingFailer{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); a.wg.Wait() })

	a.startStalePendingReconcile(ctx, repo)
	flipNamedJob(t, srv, jobStalePendingReconcile, "disable")
	for _, job := range a.backgroundStarts {
		job.start(ctx) // runs the first tick synchronously on its goroutine
	}

	h := waitForHealth(t, a, jobStalePendingReconcile, func(h JobHealth) bool { return h.Skipped >= 1 })
	if h.Enabled {
		t.Fatalf("job still enabled after admin disable: %+v", h)
	}
	if got := repo.calls.Load(); got != 0 {
		t.Fatalf("disabled stale-pending reconcile swept the repo %d times", got)
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
