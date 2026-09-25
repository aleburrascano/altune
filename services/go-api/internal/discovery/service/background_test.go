package service

import (
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// Regression test for #2244: a detached job's failure line must carry what an
// operator needs to diagnose it without a reproduction.
func TestBackgroundLaunch_PanicLogsErrorWithCorrelationIDAndStack(t *testing.T) {
	ring := captureDetachedLogs(t)
	const corrID = "corr-bg-2244"
	ctx := logging.WithCorrelationID(context.Background(), corrID)

	var bg backgroundRunner
	bg.launch(ctx, "telemetry.emit", func(context.Context) { panic("adapter exploded") })
	bg.wait()

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
