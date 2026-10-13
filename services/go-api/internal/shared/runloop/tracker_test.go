package runloop_test

import (
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/runloop"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestBackgroundLaunch_PanicLogsErrorWithCorrelationIDAndStack(t *testing.T) {
	ring := captureDetachedLogs(t)
	const corrID = "corr-bg-2244"
	ctx := logging.WithCorrelationID(context.Background(), corrID)

	var bg runloop.Tracker
	bg.Launch(ctx, "telemetry.emit", func(context.Context) { panic("adapter exploded") })
	bg.Wait()

	rec := onlyRecord(t, ring, "search.v2.background_panic")
	if rec.Level != slog.LevelError.String() {
		t.Errorf("level = %s, want ERROR: a crashed background job is not a warning", rec.Level)
	}
	if rec.Attrs["corr_id"] != corrID {
		t.Errorf("corr_id = %q, want %q: the log call must carry the detached context", rec.Attrs["corr_id"], corrID)
	}
	if !strings.Contains(rec.Attrs["stack"], "TestBackgroundLaunch_PanicLogsErrorWithCorrelationIDAndStack") {
		t.Errorf("stack = %q, want the frames of the function that panicked", rec.Attrs["stack"])
	}
	if !strings.Contains(rec.Attrs["panic"], "adapter exploded") {
		t.Errorf("panic = %q, want the recovered value", rec.Attrs["panic"])
	}
	if rec.Attrs["label"] != "telemetry.emit" {
		t.Errorf("label = %q, want telemetry.emit: the line must name which job died", rec.Attrs["label"])
	}
}

func captureDetachedLogs(t *testing.T) *logging.RingBuffer {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logging.Setup("error", false)
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
