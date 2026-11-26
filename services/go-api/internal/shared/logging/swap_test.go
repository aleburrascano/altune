package logging_test

import (
	"altune/go-api/internal/shared/logging"
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestSwap_RoutesDefaultLoggingToTheGivenLoggerUntilRestored(t *testing.T) {
	var buf bytes.Buffer
	before := slog.Default()

	restore := logging.Swap(slog.New(slog.NewTextHandler(&buf, nil)))
	slog.Info("while swapped")
	restore()

	if !strings.Contains(buf.String(), "while swapped") {
		t.Errorf("swapped logger saw %q, want the line logged while it was the default", buf.String())
	}
	if slog.Default() != before {
		t.Error("restore did not put the previous default logger back")
	}
}
