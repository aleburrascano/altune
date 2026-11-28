package service_test

import (
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func pruneLogs(t *testing.T, run func()) string {
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

var pruneNow = time.Date(2026, 11, 25, 10, 30, 0, 0, time.FixedZone("UTC+2", 2*3600))

func atPruneNow() time.Time { return pruneNow }

type recordingRetentionPruner struct {
	seen []time.Time
}

func (r *recordingRetentionPruner) Prune(_ context.Context, now time.Time) (int64, error) {
	r.seen = append(r.seen, now)
	return 0, nil
}

func TestPruneAcquisitionRows_BothPrunesAreGivenTheClocksInstantInUTC(t *testing.T) {
	outcomes, rejections := &recordingRetentionPruner{}, &recordingRetentionPruner{}

	if err := service.PruneAcquisitionRows(outcomes, rejections, atPruneNow)(context.Background()); err != nil {
		t.Fatalf("run error = %v, want nil", err)
	}

	seen := append(outcomes.seen, rejections.seen...)
	if len(seen) != 2 {
		t.Fatalf("pruners were called %d times, want 2", len(seen))
	}
	for _, got := range seen {
		if !got.Equal(pruneNow) || got.Location() != time.UTC {
			t.Errorf("pruner was given %v, want %v in UTC", got, pruneNow.UTC())
		}
	}
}

type stubRetentionPruner struct {
	pruned int64
	err    error
}

func (s stubRetentionPruner) Prune(context.Context, time.Time) (int64, error) { return s.pruned, s.err }

func TestPruneAcquisitionRows_LogsACountOnlyWhenRowsWereRemoved(t *testing.T) {
	cases := []struct {
		name                 string
		outcomes, rejections int64
		wantLog              string
	}{
		{name: "rows removed", outcomes: 3, rejections: 2, wantLog: `level=INFO msg="acquisition rows pruned" outcome_rows=3 rejection_rows=2` + "\n"},
		{name: "only rejections removed", rejections: 2, wantLog: `level=INFO msg="acquisition rows pruned" outcome_rows=0 rejection_rows=2` + "\n"},
		{name: "nothing removed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			logged := pruneLogs(t, func() {
				err = service.PruneAcquisitionRows(stubRetentionPruner{pruned: tc.outcomes}, stubRetentionPruner{pruned: tc.rejections}, atPruneNow)(context.Background())
			})

			if err != nil {
				t.Fatalf("run error = %v, want nil", err)
			}
			if logged != tc.wantLog {
				t.Errorf("logged %q, want %q", logged, tc.wantLog)
			}
		})
	}
}

func TestPruneAcquisitionRows_EachFailingPruneIsLoggedAndReturned(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name                 string
		outcomes, rejections stubRetentionPruner
		want                 string
	}{
		{"outcomes", stubRetentionPruner{err: boom}, stubRetentionPruner{}, `level=WARN msg="acquisition outcome prune failed" error=boom` + "\n"},
		{"rejections", stubRetentionPruner{}, stubRetentionPruner{err: boom}, `level=WARN msg="acquisition rejection prune failed" error=boom` + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			logged := pruneLogs(t, func() {
				err = service.PruneAcquisitionRows(tc.outcomes, tc.rejections, atPruneNow)(context.Background())
			})

			if !errors.Is(err, boom) {
				t.Errorf("run error = %v, want the prune's failure so the job counts it", err)
			}
			if !strings.Contains(logged, tc.want) {
				t.Errorf("want %q in\n%s", tc.want, logged)
			}
		})
	}
}
