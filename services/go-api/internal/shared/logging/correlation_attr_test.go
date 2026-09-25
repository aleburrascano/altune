package logging

import (
	"context"
	"log/slog"
	"testing"
)

func TestCorrelationAttr_CarriesContextID(t *testing.T) {
	ctx := WithCorrelationID(context.Background(), "corr-abc123")

	got := CorrelationAttr(ctx)

	if got.Key != "corr_id" || got.Value.String() != "corr-abc123" {
		t.Errorf("CorrelationAttr = %s=%q, want corr_id=%q", got.Key, got.Value.String(), "corr-abc123")
	}
}

func TestCorrelationAttr_KeepsEmptyIDOnTheRecord(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := Setup("debug", false)

	slog.InfoContext(context.Background(), "audit.no_request", CorrelationAttr(context.Background()))

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
