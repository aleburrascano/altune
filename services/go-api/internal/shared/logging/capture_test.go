package logging_test

import (
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"strings"
	"testing"
)

type fakeTB struct {
	cleanups []func()
}

func (*fakeTB) Helper() {}

func (f *fakeTB) Cleanup(fn func()) { f.cleanups = append(f.cleanups, fn) }

func (f *fakeTB) finish() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
}

func TestCaptureText_RecordsLinesAtOrAboveTheLevelUntilCleanup(t *testing.T) {
	tb := &fakeTB{}
	before := slog.Default()

	logs := logging.CaptureText(tb, slog.LevelWarn)
	slog.InfoContext(context.Background(), "too.quiet")
	slog.WarnContext(context.Background(), "loud.enough")
	got := logs.String()
	tb.finish()

	if strings.Contains(got, "too.quiet") || !strings.Contains(got, "loud.enough") {
		t.Errorf("captured %q, want only the warning", got)
	}
	if slog.Default() != before {
		t.Error("cleanup did not put the previous default logger back")
	}
}

func TestCaptureJSON_WritesOneJSONObjectPerLine(t *testing.T) {
	tb := &fakeTB{}
	logs := logging.CaptureJSON(tb, slog.LevelDebug)
	defer tb.finish()

	slog.DebugContext(context.Background(), "event", logging.KeyTrackID, "t-1")

	if got := logs.String(); !strings.Contains(got, `"track_id":"t-1"`) {
		t.Errorf("captured %q, want a JSON line carrying track_id", got)
	}
}

func TestSilence_DropsLinesUntilCleanup(t *testing.T) {
	tb := &fakeTB{}
	before := slog.Default()

	logging.Silence(tb)
	if slog.Default().Enabled(context.Background(), slog.LevelError) {
		t.Error("silenced logger is still enabled")
	}
	tb.finish()

	if slog.Default() != before {
		t.Error("cleanup did not put the previous default logger back")
	}
}
