package service_test

import (
	"altune/go-api/internal/platform/monitoring/internal/service"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

func retentionLogs(t *testing.T, run func()) string {
	t.Helper()
	buf := &logging.Capture{}
	logging.UseHandler(t, slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo, ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) == 0 && a.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return a
	}}))
	run()
	return buf.String()
}

func atTestNow() time.Time { return testNow }

type timeRecordingPruner struct {
	seen []time.Time
}

func (r *timeRecordingPruner) Prune(_ context.Context, now time.Time) (int64, error) {
	r.seen = append(r.seen, now)
	return 0, nil
}

type fixedPruner struct {
	rows int64
	err  error
}

func (f fixedPruner) Prune(context.Context, time.Time) (int64, error) { return f.rows, f.err }

func TestPruneClientEvents_IsGivenTheClocksInstantInUTC(t *testing.T) {
	pruner := &timeRecordingPruner{}

	if err := service.PruneClientEvents(pruner, atTestNow)(context.Background()); err != nil {
		t.Fatalf("run error = %v, want nil", err)
	}

	if len(pruner.seen) != 1 {
		t.Fatalf("pruners were called %d times, want 1", len(pruner.seen))
	}
	if got := pruner.seen[0]; !got.Equal(testNow) || got.Location() != time.UTC {
		t.Errorf("pruner was given %v, want %v in UTC", got, testNow.UTC())
	}
}

func TestPruneClientEvents_LogsTheCountOnlyWhenRowsWereRemovedAndSurfacesFailure(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name    string
		pruner  fixedPruner
		wantErr bool
		wantLog string
	}{
		{"rows removed", fixedPruner{rows: 4}, false, `level=INFO msg="client events pruned" rows=4` + "\n"},
		{"nothing removed", fixedPruner{}, false, ""},
		{"the prune fails", fixedPruner{err: boom}, true, `level=WARN msg="client event retention prune failed" error=boom` + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			logged := retentionLogs(t, func() { err = service.PruneClientEvents(tc.pruner, atTestNow)(context.Background()) })

			if tc.wantErr != errors.Is(err, boom) {
				t.Errorf("run error = %v, want the prune's failure surfaced: %v", err, tc.wantErr)
			}
			if logged != tc.wantLog {
				t.Errorf("logged %q, want %q", logged, tc.wantLog)
			}
		})
	}
}
