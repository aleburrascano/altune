package background_test

import (
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestTry_PanicBecomesAPanicErrorAndOneErrorLogWithTheRequestID(t *testing.T) {
	ring := captureDetachedLogs(t)
	ctx := logging.ContextWithCorrelationID(context.Background(), "corr-try-7")

	err := background.SafeRun(ctx, "test.try_panic", func() { panic("adapter exploded") })

	var panicked *background.PanicError
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

	if err := background.SafeRun(context.Background(), "test.try_panic", func() { ran = true }); err != nil {
		t.Fatalf("Try() = %v, want nil", err)
	}
	if !ran {
		t.Fatal("Try did not run fn")
	}
	if got := len(ring.Snapshot()); got != 0 {
		t.Errorf("captured %d records, want none", got)
	}
}

func TestTry_LogsTheCallersAttributes(t *testing.T) {
	ring := captureDetachedLogs(t)

	_ = background.SafeRun(context.Background(), "test.try_attrs", func() { panic("boom") }, "provider", "deezer")

	if got := onlyRecord(t, ring, "test.try_attrs").Attrs["provider"]; got != "deezer" {
		t.Errorf("provider = %q, want deezer", got)
	}
}

func TestTry_RedactsSecretsInThePanicValue(t *testing.T) {
	ring := captureDetachedLogs(t)

	err := background.SafeRun(context.Background(), "test.try_redact", func() {
		panic("GET https://api.example.test/v1?token=s3cr3t-value failed")
	})

	logged := onlyRecord(t, ring, "test.try_redact").Attrs["panic"]
	for name, text := range map[string]string{"logged panic": logged, "Error()": err.Error()} {
		if strings.Contains(text, "s3cr3t-value") || !strings.Contains(text, "REDACTED") {
			t.Errorf("%s = %q, want the token masked", name, text)
		}
	}
}
