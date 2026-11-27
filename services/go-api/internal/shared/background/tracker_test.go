package background_test

import (
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func captureDetachedLogs(t *testing.T) *logging.RingBuffer {
	t.Helper()
	return captureLogsAt(t, "error")
}

func captureLogsAt(t *testing.T, level string) *logging.RingBuffer {
	t.Helper()
	return logging.CaptureRing(t, level)
}

func onlyRecord(t *testing.T, ring *logging.RingBuffer, msg string) logging.CapturedRecord {
	t.Helper()
	var found []logging.CapturedRecord
	for _, r := range ring.Snapshot() {
		if r.Message == msg {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		t.Fatalf("captured %d %q records, want exactly 1", len(found), msg)
	}
	return found[0]
}

func newTracker(t *testing.T, maxInFlight int) *background.Tracker {
	t.Helper()
	bg, err := background.NewTracker(background.TrackerLimits{MaxInFlight: maxInFlight})
	if err != nil {
		t.Fatalf("NewTracker(%d): %v", maxInFlight, err)
	}
	return bg
}

func mustLaunch(t *testing.T, bg *background.Tracker, parent context.Context, label string, budget time.Duration, fn func(context.Context)) {
	t.Helper()
	if !bg.LaunchWithin(parent, label, budget, fn) {
		t.Fatalf("LaunchWithin(%q) was refused", label)
	}
}

func drainWithin(t *testing.T, bg *background.Tracker, wait time.Duration) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	return bg.Drain(ctx)
}

func TestNewTracker_RefusesACapBelowOne(t *testing.T) {
	for _, maxInFlight := range []int{0, -1} {
		if _, err := background.NewTracker(background.TrackerLimits{MaxInFlight: maxInFlight}); !errors.Is(err, background.ErrTrackerLimit) {
			t.Errorf("NewTracker(%d) error = %v, want ErrTrackerLimit", maxInFlight, err)
		}
	}
}

func TestLaunchWithin_AtTheCapDropsAndCountsTheTask(t *testing.T) {
	bg := newTracker(t, 1)
	release := make(chan struct{})
	if !bg.LaunchWithin(context.Background(), "first", time.Minute, func(context.Context) { <-release }) {
		t.Fatal("the first task was refused below the cap")
	}
	var ranSecond atomic.Bool
	if bg.LaunchWithin(context.Background(), "second", time.Minute, func(context.Context) { ranSecond.Store(true) }) {
		t.Fatal("a second task was accepted at a cap of 1")
	}
	if got := bg.Dropped(); got != 1 {
		t.Errorf("Dropped() = %d, want 1", got)
	}
	close(release)
	if !drainWithin(t, bg, 5*time.Second) {
		t.Fatal("Drain timed out after the task was released")
	}
	if ranSecond.Load() {
		t.Error("the dropped task ran")
	}
}

func TestLaunchWithin_AFinishedTaskFreesItsSlot(t *testing.T) {
	bg := newTracker(t, 1)
	mustLaunch(t, bg, context.Background(), "first", time.Minute, func(context.Context) {})
	deadline := time.Now().Add(5 * time.Second)
	for !bg.LaunchWithin(context.Background(), "next", time.Minute, func(context.Context) {}) {
		if time.Now().After(deadline) {
			t.Fatal("the slot was never freed after the first task finished")
		}
		time.Sleep(time.Millisecond)
	}
	if !drainWithin(t, bg, 5*time.Second) {
		t.Fatal("Drain timed out")
	}
}

type launchedCtx struct {
	deadline time.Time
	err      error
	corrID   string
}

func TestLaunchWithin_RunsPastTheRequestUnderItsOwnBudget(t *testing.T) {
	bg := newTracker(t, 1)
	parent, cancel := context.WithCancel(logging.ContextWithCorrelationID(context.Background(), "corr-bg-77"))
	seen := make(chan launchedCtx, 1)
	before := time.Now()
	mustLaunch(t, bg, parent, "telemetry.emit", 2*time.Second, func(ctx context.Context) {
		cancel()
		deadline, _ := ctx.Deadline()
		seen <- launchedCtx{deadline: deadline, err: ctx.Err(), corrID: logging.CorrelationIDFromContext(ctx)}
	})
	got := <-seen
	after := time.Now()
	if got.err != nil {
		t.Errorf("task ctx err = %v after the request was canceled, want nil", got.err)
	}
	if got.deadline.Before(before.Add(2*time.Second)) || got.deadline.After(after.Add(2*time.Second)) {
		t.Errorf("deadline = %v, want 2s past the launch", got.deadline)
	}
	if got.corrID != "corr-bg-77" {
		t.Errorf("corr id = %q, want the request's", got.corrID)
	}
	drainWithin(t, bg, 5*time.Second)
}

func TestLaunchWithin_APanicIsLoggedWithItsLabelAndTheTaskStillEnds(t *testing.T) {
	ring := captureDetachedLogs(t)
	bg := newTracker(t, 1)
	ctx := logging.ContextWithCorrelationID(context.Background(), "corr-bg-88")
	mustLaunch(t, bg, ctx, "vocab.ingest", time.Minute, func(context.Context) { panic("store exploded") })
	if !drainWithin(t, bg, 5*time.Second) {
		t.Fatal("Drain timed out: a panicking task must still end")
	}
	rec := onlyRecord(t, ring, "background.task_panic")
	if rec.Attrs["label"] != "vocab.ingest" || rec.Attrs["corr_id"] != "corr-bg-88" {
		t.Errorf("attrs = %v, want label vocab.ingest and corr_id corr-bg-88", rec.Attrs)
	}
}

func TestDrain_IsFalseWhileATaskRunsPastTheDeadline(t *testing.T) {
	bg := newTracker(t, 2)
	release := make(chan struct{})
	mustLaunch(t, bg, context.Background(), "slow", time.Minute, func(context.Context) { <-release })
	if drainWithin(t, bg, 50*time.Millisecond) {
		t.Error("Drain = true with a task still running")
	}
	close(release)
	if !drainWithin(t, bg, 5*time.Second) {
		t.Error("Drain = false after the task finished")
	}
}

func TestDrain_RefusesTasksLaunchedAfterItStarts(t *testing.T) {
	bg := newTracker(t, 2)
	if !drainWithin(t, bg, time.Second) {
		t.Fatal("Drain of an idle tracker = false")
	}
	var ran atomic.Bool
	if bg.LaunchWithin(context.Background(), "late", time.Minute, func(context.Context) { ran.Store(true) }) {
		t.Error("a task launched after Drain was accepted")
	}
	if got := bg.Dropped(); got != 1 {
		t.Errorf("Dropped() = %d, want 1", got)
	}
	if ran.Load() {
		t.Error("the refused task ran")
	}
}

func TestTrack_RunsEvenWhenTheLaunchCapIsFull(t *testing.T) {
	bg := newTracker(t, 1)
	release := make(chan struct{})
	mustLaunch(t, bg, context.Background(), "slow", time.Minute, func(context.Context) { <-release })
	ran := make(chan struct{})
	bg.Track(func() { close(ran) })
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Error("Track did not run while the launch cap was full")
	}
	close(release)
	drainWithin(t, bg, 5*time.Second)
}

func TestTrack_StillRunsAfterDrain(t *testing.T) {
	bg := newTracker(t, 1)
	drainWithin(t, bg, time.Second)
	ran := make(chan struct{})
	bg.Track(func() { close(ran) })
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Error("Track after Drain did not run: in-request work must not be dropped")
	}
}

func TestLaunchWithin_ADroppedTaskIsLoggedAtWarnWithItsLabel(t *testing.T) {
	ring := captureLogsAt(t, "warn")
	bg := newTracker(t, 1)
	release := make(chan struct{})
	mustLaunch(t, bg, context.Background(), "slow", time.Minute, func(context.Context) { <-release })

	bg.LaunchWithin(logging.ContextWithCorrelationID(context.Background(), "corr-bg-99"), "artwork_fill", time.Minute, func(context.Context) {})

	rec := onlyRecord(t, ring, "background.task_dropped")
	if rec.Level != slog.LevelWarn.String() || rec.Attrs["label"] != "artwork_fill" || rec.Attrs["corr_id"] != "corr-bg-99" {
		t.Errorf("record = %s %v, want WARN with label artwork_fill and corr_id corr-bg-99", rec.Level, rec.Attrs)
	}
	close(release)
	drainWithin(t, bg, 5*time.Second)
}

func TestTrack_HoldsNoLaunchSlot(t *testing.T) {
	bg := newTracker(t, 1)
	release := make(chan struct{})
	bg.Track(func() { <-release })
	mustLaunch(t, bg, context.Background(), "next", time.Minute, func(context.Context) {})
	close(release)
	if !drainWithin(t, bg, 5*time.Second) {
		t.Error("Drain timed out")
	}
}
