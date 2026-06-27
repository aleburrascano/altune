package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestRing_CapturesDebugBelowStdoutLevel verifies the console ring retains DEBUG
// records even when stdout is at INFO, while stdout itself stays at INFO.
func TestRing_CapturesDebugBelowStdoutLevel(t *testing.T) {
	ring := NewRingBuffer(10)
	var stdout bytes.Buffer
	inner := slog.NewJSONHandler(&stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(newRingHandler(inner, ring))

	logger.Debug("provider.debug.line", "provider", "deezer")
	logger.Info("request.complete", "status", 200)

	snap := ring.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("ring should retain both DEBUG and INFO, got %d", len(snap))
	}

	out := stdout.String()
	if strings.Contains(out, "provider.debug.line") {
		t.Error("DEBUG must not reach stdout when stdout level is INFO")
	}
	if !strings.Contains(out, "request.complete") {
		t.Error("INFO must still reach stdout")
	}
}
