package logging

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

// countingHandler records the corr_id attribute value of each handled record,
// so a test can assert exactly how many reach the inner handler.
type countingHandler struct {
	corrValues []string
}

func (*countingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *countingHandler) Handle(_ context.Context, r slog.Record) error {
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "corr_id" {
			h.corrValues = append(h.corrValues, a.Value.String())
		}
		return true
	})
	return nil
}

func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(string) slog.Handler      { return h }

// TestCorrelationHandler_StampsEveryContextLog proves the generalized fix:
// any *Context log call made under a correlation context is tagged, without
// the call site adding corr_id itself.
func TestCorrelationHandler_StampsEveryContextLog(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := Setup("debug", false)

	ctx := WithCorrelationID(context.Background(), "corr-xyz789")
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

// TestCorrelationHandler_NoIDLeavesRecordUntagged guards against stamping an
// empty corr_id onto logs made outside a request (background jobs, startup).
func TestCorrelationHandler_NoIDLeavesRecordUntagged(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := Setup("debug", false)

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

// TestCorrelationHandler_DoesNotDuplicate ensures a call site that already set
// corr_id is left alone, so the request-level logs carry exactly one.
func TestCorrelationHandler_DoesNotDuplicate(t *testing.T) {
	base := &countingHandler{}
	h := newCorrelationHandler(base)
	ctx := WithCorrelationID(context.Background(), "from-ctx")

	rec := slog.NewRecord(time.Time{}, slog.LevelInfo, "msg", 0)
	rec.AddAttrs(slog.String("corr_id", "explicit"))
	if err := h.Handle(ctx, rec); err != nil {
		t.Fatalf("handle: %v", err)
	}

	if got := base.corrValues; len(got) != 1 || got[0] != "explicit" {
		t.Errorf("corr_id attrs seen = %v, want exactly [explicit]", got)
	}
}
