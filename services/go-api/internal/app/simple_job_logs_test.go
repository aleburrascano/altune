package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

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
