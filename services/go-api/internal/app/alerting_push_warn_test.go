package app

import (
	"altune/go-api/internal/shared/config"
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestStartAlertMonitor_WarnsWhenPushUnconfigured(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	a := &App{cfg: &config.Config{}}

	a.startAlertMonitor(context.Background())

	out := buf.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "alert push not configured") {
		t.Fatalf("log = %q, want a WARN that push is not configured", out)
	}
}
