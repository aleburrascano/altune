package logging_test

import (
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"
)

var ansiColor = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func newPrettyLogger(t *testing.T, level string) (*slog.Logger, func() []string) {
	t.Helper()
	logger, _, out := setupLogger(t, level, true)
	return logger, func() []string {
		plain := strings.TrimSuffix(ansiColor.ReplaceAllString(out(), ""), "\n")
		if plain == "" {
			return nil
		}
		return strings.Split(plain, "\n")
	}
}

var prettyLine = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}\.\d{3} (ERROR|WARN |INFO |DEBUG) `)

func TestPrettyHandler_WritesOneColoredLinePerRecordWithItsLevel(t *testing.T) {
	logger, lines := newPrettyLogger(t, "debug")

	logger.Debug("d")
	logger.Info("i")
	logger.Warn("w")
	logger.Error("e")

	got := lines()
	want := []string{"DEBUG d", "INFO  i", "WARN  w", "ERROR e"}
	if len(got) != len(want) {
		t.Fatalf("lines = %q, want %d lines", got, len(want))
	}
	for i, line := range got {
		if !prettyLine.MatchString(line) || !strings.Contains(line, want[i]+" ") {
			t.Errorf("line %d = %q, want a timestamp then %q", i, line, want[i])
		}
	}
}

func TestPrettyHandler_WritesTheCallerFunctionAndFile(t *testing.T) {
	logger, lines := newPrettyLogger(t, "info")

	logger.Info("where")

	got := lines()
	if len(got) != 1 || !strings.Contains(got[0], " @TestPrettyHandler_WritesTheCallerFunctionAndFile logging/pretty_handler_test.go:") {
		t.Fatalf("lines = %q, want the caller as @function and dir/file:line", got)
	}
}

func TestPrettyHandler_SkipsRecordsBelowItsLevelAndKeepsThoseAtIt(t *testing.T) {
	logger, lines := newPrettyLogger(t, "warn")

	logger.Info("below")
	logger.Warn("at")

	got := lines()
	if len(got) != 1 || !strings.Contains(got[0], "WARN  at") {
		t.Fatalf("lines = %q, want only the WARN record", got)
	}
}

func TestPrettyHandler_WritesAttrsAsKeyValueWithGroupPrefixes(t *testing.T) {
	logger, lines := newPrettyLogger(t, "info")
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	logger.With("bound", 1).WithGroup("g").With("k", 2).Info("attrs",
		"r", 3,
		slog.Group("p", "n", "x"),
		slog.Time("at", at),
		slog.Duration("took", 1500*time.Millisecond),
	)

	got := lines()
	if len(got) != 1 {
		t.Fatalf("lines = %q, want one", got)
	}
	for _, want := range []string{" bound=1", " g.k=2", " g.r=3", " g.p.n=x", " g.at=2026-01-02T03:04:05Z", " g.took=1.5s"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("line %q lacks %q", got[0], want)
		}
	}
}

func TestPrettyHandler_EmptyGroupNameAddsNoPrefix(t *testing.T) {
	logger, lines := newPrettyLogger(t, "info")

	slog.New(logger.Handler().WithGroup("")).Info("ungrouped", "k", 1)

	got := lines()
	if len(got) != 1 || !strings.Contains(got[0], " k=1") || strings.Contains(got[0], ".k=1") {
		t.Fatalf("lines = %q, want k=1 with no group prefix", got)
	}
}
