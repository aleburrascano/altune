package runloop_test

import (
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/runloop"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestTry_PanicBecomesAPanicErrorAndOneErrorLogWithTheRequestID(t *testing.T) {
	ring := captureDetachedLogs(t)
	ctx := logging.WithCorrelationID(context.Background(), "corr-try-7")

	err := runloop.Try(ctx, "test.try_panic", func() { panic("adapter exploded") })

	var panicked *runloop.PanicError
	if !errors.As(err, &panicked) {
		t.Fatalf("Try() = %v, want a *runloop.PanicError", err)
	}
	if panicked.Event != "test.try_panic" || panicked.Value != "adapter exploded" {
		t.Errorf("PanicError = {Event: %q, Value: %v}, want the event and the panic value", panicked.Event, panicked.Value)
	}
	if !strings.Contains(string(panicked.Stack), "TestTry_PanicBecomesAPanicError") {
		t.Errorf("Stack = %q, want the frames of the function that panicked", panicked.Stack)
	}
	if want := "test.try_panic: panic: adapter exploded"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	rec := onlyRecord(t, ring, "test.try_panic")
	if rec.Level != slog.LevelError.String() {
		t.Errorf("level = %s, want ERROR", rec.Level)
	}
	if rec.Attrs["corr_id"] != "corr-try-7" {
		t.Errorf("corr_id = %q, want the request id the caller passed", rec.Attrs["corr_id"])
	}
	if !strings.Contains(rec.Attrs["stack"], "TestTry_PanicBecomesAPanicError") {
		t.Errorf("logged stack = %q, want the panicking frames", rec.Attrs["stack"])
	}
}

func TestTry_ReturnsNilAndLogsNothingWhenFnReturns(t *testing.T) {
	ring := captureDetachedLogs(t)
	ran := false

	if err := runloop.Try(context.Background(), "test.try_panic", func() { ran = true }); err != nil {
		t.Fatalf("Try() = %v, want nil", err)
	}
	if !ran {
		t.Fatal("Try did not run fn")
	}
	if got := len(ring.Snapshot()); got != 0 {
		t.Errorf("captured %d records, want none", got)
	}
}
