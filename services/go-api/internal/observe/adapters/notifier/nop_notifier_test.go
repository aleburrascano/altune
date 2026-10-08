package notifier

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"altune/go-api/internal/observe/domain"
)

func TestNopNotifier(t *testing.T) {
	if err := (NopNotifier{}).Notify(context.Background(), domain.Alert{}); err != nil {
		t.Fatalf("NopNotifier.Notify returned %v, want nil", err)
	}
}

func TestNopNotifier_LogsFiringAlertAtError(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	if err := (NopNotifier{}).Notify(context.Background(), domain.Alert{Title: "dep down", Message: "state down"}); err != nil {
		t.Fatalf("NopNotifier.Notify returned %v, want nil", err)
	}

	out := buf.String()
	if !strings.Contains(out, "level=ERROR") {
		t.Fatalf("log = %q, want an ERROR-level line", out)
	}
	if !strings.Contains(out, "dep down") || !strings.Contains(out, "state down") {
		t.Fatalf("log = %q, want it to carry the alert title and message", out)
	}
}
