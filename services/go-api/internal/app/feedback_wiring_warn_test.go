package app

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"altune/go-api/internal/shared/config"
)

func TestWireFeedback_WarnsWhenEnabledButUnconfigured(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	a := &App{cfg: &config.Config{FeedbackEnabled: true}}

	if a.wireFeedback() != nil {
		t.Fatal("expected no handler without credentials")
	}
	out := buf.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "GITHUB_ISSUE_TOKEN") {
		t.Fatalf("log = %q, want a WARN naming the variables", out)
	}
}
