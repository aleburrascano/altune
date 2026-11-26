package cleanupjobs_test

import (
	"altune/go-api/internal/app/cleanupjobs"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func captureLogs(t *testing.T, run func()) string {
	t.Helper()
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo, ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) == 0 && a.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return a
	}})))
	defer slog.SetDefault(restore)
	run()
	return buf.String()
}

var testNow = time.Date(2026, 11, 25, 10, 30, 0, 0, time.FixedZone("UTC+2", 2*3600))

func atTestNow() time.Time { return testNow }

type timeRecordingPruner struct {
	seen []time.Time
}

func (r *timeRecordingPruner) PruneDiscographyObserved(_ context.Context, now time.Time) (int64, error) {
	r.seen = append(r.seen, now)
	return 0, nil
}

func (r *timeRecordingPruner) PruneEvents(_ context.Context, now time.Time) (int64, error) {
	r.seen = append(r.seen, now)
	return 0, nil
}

func (r *timeRecordingPruner) Prune(_ context.Context, now time.Time) (int64, error) {
	r.seen = append(r.seen, now)
	return 0, nil
}

func assertAllAtTestNowInUTC(t *testing.T, seen []time.Time, wantCalls int) {
	t.Helper()
	if len(seen) != wantCalls {
		t.Fatalf("pruners were called %d times, want %d", len(seen), wantCalls)
	}
	for _, got := range seen {
		if !got.Equal(testNow) || got.Location() != time.UTC {
			t.Errorf("pruner was given %v, want %v in UTC", got, testNow.UTC())
		}
	}
}

func TestPruneDiscoveryEvents_BothPrunesAreGivenTheClocksInstantInUTC(t *testing.T) {
	pruner := &timeRecordingPruner{}

	if err := cleanupjobs.PruneDiscoveryEvents(pruner, atTestNow)(context.Background()); err != nil {
		t.Fatalf("run error = %v, want nil", err)
	}

	assertAllAtTestNowInUTC(t, pruner.seen, 2)
}

func TestPruneAcquisitionRows_BothPrunesAreGivenTheClocksInstantInUTC(t *testing.T) {
	outcomes, rejections := &timeRecordingPruner{}, &timeRecordingPruner{}

	if err := cleanupjobs.PruneAcquisitionRows(outcomes, rejections, atTestNow)(context.Background()); err != nil {
		t.Fatalf("run error = %v, want nil", err)
	}

	assertAllAtTestNowInUTC(t, append(outcomes.seen, rejections.seen...), 2)
}

type failingPruner struct {
	discographyErr, evtErr error
}

func (f failingPruner) PruneDiscographyObserved(context.Context, time.Time) (int64, error) {
	return 0, f.discographyErr
}

func (f failingPruner) PruneEvents(context.Context, time.Time) (int64, error) {
	return 0, f.evtErr
}

func TestPruneDiscoveryEvents_LogsTheOldTextOnEachFailurePath(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name       string
		pruner     failingPruner
		wantFailed string
		notWant    string
	}{
		{
			name:       "discography prune fails",
			pruner:     failingPruner{discographyErr: boom},
			wantFailed: `level=WARN msg="discography event prune failed" error=boom` + "\n",
			notWant:    "discovery event retention prune failed",
		},
		{
			name:       "event prune fails",
			pruner:     failingPruner{evtErr: boom},
			wantFailed: `level=WARN msg="discovery event retention prune failed" error=boom` + "\n",
			notWant:    "discography event prune failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			logged := captureLogs(t, func() { err = cleanupjobs.PruneDiscoveryEvents(tc.pruner, atTestNow)(context.Background()) })

			if !errors.Is(err, boom) {
				t.Errorf("run error = %v, want the prune's failure so the job counts it", err)
			}
			if !strings.Contains(logged, tc.wantFailed) {
				t.Errorf("want %q in\n%s", tc.wantFailed, logged)
			}
			if strings.Contains(logged, tc.notWant) {
				t.Errorf("unexpected %q in\n%s", tc.notWant, logged)
			}
		})
	}
}

type stubPruner struct {
	pruned int64
	err    error
}

func (s stubPruner) Prune(context.Context, time.Time) (int64, error) { return s.pruned, s.err }

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
			logged := captureLogs(t, func() {
				err = cleanupjobs.PruneAcquisitionRows(stubPruner{pruned: tc.outcomes}, stubPruner{pruned: tc.rejections}, atTestNow)(context.Background())
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
		outcomes, rejections stubPruner
		want                 string
	}{
		{"outcomes", stubPruner{err: boom}, stubPruner{}, `level=WARN msg="acquisition outcome prune failed" error=boom` + "\n"},
		{"rejections", stubPruner{}, stubPruner{err: boom}, `level=WARN msg="acquisition rejection prune failed" error=boom` + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			logged := captureLogs(t, func() {
				err = cleanupjobs.PruneAcquisitionRows(tc.outcomes, tc.rejections, atTestNow)(context.Background())
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

type countedPruner struct {
	discography, other int64
}

func (c countedPruner) PruneDiscographyObserved(context.Context, time.Time) (int64, error) {
	return c.discography, nil
}

func (c countedPruner) PruneEvents(context.Context, time.Time) (int64, error) {
	return c.other, nil
}

func TestPruneDiscoveryEvents_LogsBothCountsOnlyWhenRowsWereRemoved(t *testing.T) {
	cases := []struct {
		name    string
		pruner  countedPruner
		wantLog string
	}{
		{"both kinds removed", countedPruner{discography: 3, other: 2}, `level=INFO msg="discovery events pruned" discography_rows=3 other_rows=2` + "\n"},
		{"only other events removed", countedPruner{other: 2}, `level=INFO msg="discovery events pruned" discography_rows=0 other_rows=2` + "\n"},
		{"nothing removed", countedPruner{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			logged := captureLogs(t, func() { err = cleanupjobs.PruneDiscoveryEvents(tc.pruner, atTestNow)(context.Background()) })

			if err != nil {
				t.Fatalf("run error = %v, want nil", err)
			}
			if logged != tc.wantLog {
				t.Errorf("logged %q, want %q", logged, tc.wantLog)
			}
		})
	}
}
