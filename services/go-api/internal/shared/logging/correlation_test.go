package logging_test

import (
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestCorrelationHandler_StampsEveryContextLog(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("debug")

	ctx := logging.ContextWithCorrelationID(context.Background(), "corr-xyz789")
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
	ring := logging.Setup("debug")

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

func TestCorrelationHandler_StampsTheJobRunAndKeepsAnExplicitJob(t *testing.T) {
	logger, _, out := setupLogger(t, "info", false)
	ctx := logging.ContextWithJobRun(context.Background(), "reconcile", "run-1")

	logger.InfoContext(ctx, "stamped")
	logger.InfoContext(ctx, "explicit", "job", "other")

	lines := strings.Split(strings.TrimSpace(out()), "\n")
	if len(lines) != 2 {
		t.Fatalf("wrote %d lines, want 2: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], `"job":"reconcile"`) || !strings.Contains(lines[0], `"run_id":"run-1"`) {
		t.Errorf("line carries no job and run_id from the context: %s", lines[0])
	}
	if got := strings.Count(lines[1], `"job"`); got != 1 || !strings.Contains(lines[1], `"job":"other"`) || !strings.Contains(lines[1], `"run_id":"run-1"`) {
		t.Errorf("line has %d job keys, want one holding other beside run_id run-1: %s", got, lines[1])
	}
}

func TestCorrelationHandler_DoesNotDuplicate(t *testing.T) {
	logger, _, out := setupLogger(t, "info", false)
	ctx := logging.ContextWithCorrelationID(context.Background(), "from-ctx")

	logger.InfoContext(ctx, "msg", "corr_id", "explicit")

	line := out()
	if got := strings.Count(line, `"corr_id"`); got != 1 || !strings.Contains(line, `"corr_id":"explicit"`) {
		t.Errorf("written line has %d corr_id keys, want exactly one holding explicit: %s", got, line)
	}
}

func TestCorrelationHandler_StampsLogsThroughDerivedLoggers(t *testing.T) {
	logger, ring := newCaptureLogger(t)
	ctx := logging.ContextWithCorrelationID(context.Background(), "derived-corr")

	logger.With("k", "v").InfoContext(ctx, "derived")

	snap := ring.Snapshot()
	if len(snap) != 1 || snap[0].Attrs["k"] != "v" || snap[0].Attrs["corr_id"] != "derived-corr" {
		t.Fatalf("derived logger record = %+v, want k=v and the correlation id", snap)
	}
}

func TestCorrelationAttr_CarriesContextID(t *testing.T) {
	ctx := logging.ContextWithCorrelationID(context.Background(), "corr-abc123")

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
	ring := logging.Setup("debug")

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

const sampleTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"

func contextInSpan(t *testing.T) context.Context {
	t.Helper()
	traceID, err := trace.TraceIDFromHex(sampleTraceID)
	if err != nil {
		t.Fatalf("trace id: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatalf("span id: %v", err)
	}
	span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled})
	return trace.ContextWithSpanContext(context.Background(), span)
}

func TestCorrelationHandler_StampsTheTraceIDOfTheSpanInContext(t *testing.T) {
	logger, ring := newCaptureLogger(t)

	logger.InfoContext(contextInSpan(t), "traced.event")

	snap := ring.Snapshot()
	if len(snap) != 1 || snap[0].Attrs["trace_id"] != sampleTraceID {
		t.Fatalf("records = %+v, want one carrying trace_id %s", snap, sampleTraceID)
	}
}

func TestCorrelationHandler_LeavesTraceIDOffOutsideASpan(t *testing.T) {
	logger, ring := newCaptureLogger(t)

	logger.InfoContext(context.Background(), "untraced.event")

	snap := ring.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("records = %+v, want one", snap)
	}
	if got, ok := snap[0].Attrs["trace_id"]; ok {
		t.Errorf("trace_id = %q on a log outside any span, want none", got)
	}
}

func TestCorrelationHandler_StampsTraceAndCorrelationIDsTogether(t *testing.T) {
	logger, ring := newCaptureLogger(t)
	ctx := logging.ContextWithCorrelationID(contextInSpan(t), "corr-both")

	logger.InfoContext(ctx, "both.ids")

	snap := ring.Snapshot()
	if len(snap) != 1 || snap[0].Attrs["trace_id"] != sampleTraceID || snap[0].Attrs["corr_id"] != "corr-both" {
		t.Fatalf("records = %+v, want trace_id %s and corr_id corr-both", snap, sampleTraceID)
	}
}

func TestCorrelationHandler_KeepsAnExplicitTraceID(t *testing.T) {
	logger, _, out := setupLogger(t, "info", false)

	logger.InfoContext(contextInSpan(t), "msg", "trace_id", "explicit")

	line := out()
	if got := strings.Count(line, `"trace_id"`); got != 1 || !strings.Contains(line, `"trace_id":"explicit"`) {
		t.Errorf("written line has %d trace_id keys, want exactly one holding explicit: %s", got, line)
	}
}
