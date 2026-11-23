package logging_test

import (
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestCorrelationHandler_StampsEveryContextLog(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("debug", false)

	ctx := logging.WithCorrelationID(context.Background(), "corr-xyz789")
	slog.InfoContext(ctx, "deep.domain.event", "detail", "v")

	found := false
	for _, r := range ring.Snapshot() {
		if r.Message != "deep.domain.event" {
			continue
		}
		found = true
		if r.Attrs["corr_id"] != "corr-xyz789" {
			t.Errorf("corr_id = %q, want %q", r.Attrs["corr_id"], "corr-xyz789")
		}
	}
	if !found {
		t.Fatal("expected deep.domain.event captured so the assertion is meaningful")
	}
}

func TestCorrelationHandler_NoIDLeavesRecordUntagged(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("debug", false)

	slog.InfoContext(context.Background(), "no.correlation")

	for _, r := range ring.Snapshot() {
		if r.Message == "no.correlation" {
			if _, ok := r.Attrs["corr_id"]; ok {
				t.Errorf("corr_id present %q on a log with no correlation context", r.Attrs["corr_id"])
			}
			return
		}
	}
	t.Fatal("expected no.correlation captured")
}

func TestCorrelationHandler_DoesNotDuplicate(t *testing.T) {
	logger, _, out := setupLogger(t, "info", false)
	ctx := logging.WithCorrelationID(context.Background(), "from-ctx")

	logger.InfoContext(ctx, "msg", "corr_id", "explicit")

	line := out()
	if got := strings.Count(line, `"corr_id"`); got != 1 || !strings.Contains(line, `"corr_id":"explicit"`) {
		t.Errorf("written line has %d corr_id keys, want exactly one holding explicit: %s", got, line)
	}
}

func TestCorrelationHandler_StampsLogsThroughDerivedLoggers(t *testing.T) {
	logger, ring := newCaptureLogger(t)
	ctx := logging.WithCorrelationID(context.Background(), "derived-corr")

	logger.With("k", "v").InfoContext(ctx, "derived")

	snap := ring.Snapshot()
	if len(snap) != 1 || snap[0].Attrs["k"] != "v" || snap[0].Attrs["corr_id"] != "derived-corr" {
		t.Fatalf("derived logger record = %+v, want k=v and the correlation id", snap)
	}
}

func TestCorrelationAttr_CarriesContextID(t *testing.T) {
	ctx := logging.WithCorrelationID(context.Background(), "corr-abc123")

	got := logging.CorrelationAttr(ctx)

	if got.Key != "corr_id" || got.Value.String() != "corr-abc123" {
		t.Errorf("CorrelationAttr = %s=%q, want corr_id=%q", got.Key, got.Value.String(), "corr-abc123")
	}
}

func TestCorrelationIDFromContext_EmptyWithoutAnID(t *testing.T) {
	if got := logging.CorrelationIDFromContext(context.Background()); got != "" {
		t.Errorf("CorrelationIDFromContext = %q, want empty", got)
	}
}

func TestCorrelationAttr_KeepsEmptyIDOnTheRecord(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("debug", false)

	slog.InfoContext(context.Background(), "audit.no_request", logging.CorrelationAttr(context.Background()))

	for _, r := range ring.Snapshot() {
		if r.Message != "audit.no_request" {
			continue
		}
		if id, ok := r.Attrs["corr_id"]; !ok || id != "" {
			t.Errorf("corr_id = %q (present %v), want present and empty", id, ok)
		}
		return
	}
	t.Fatal("expected audit.no_request captured")
}
