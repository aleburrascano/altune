package ytdlp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeFfmpegProber returns a prober whose ffmpeg is the given shell script and
// whose decode deadline is short enough for a test to outrun.
func fakeFfmpegProber(t *testing.T, script string, decodeTimeout time.Duration) *FfprobeProber {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	p := NewFfprobeProber("")
	p.ffmpeg = path
	p.decodeTimeout = decodeTimeout
	return p
}

func captureDefaultLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// TestFfprobeProber_ValidateDecodable_AcceptsADecodeThatOutranItsDeadline
// guards issue #1987: the deadline kills ffmpeg, so cmd.Run reports a signal
// exit that used to be read as proof the file was undecodable, rejecting a good
// download permanently.
func TestFfprobeProber_ValidateDecodable_AcceptsADecodeThatOutranItsDeadline(t *testing.T) {
	logs := captureDefaultLogs(t)
	p := fakeFfmpegProber(t, "#!/bin/sh\nsleep 30\n", 100*time.Millisecond)

	err := p.ValidateDecodable(context.Background(), "/tmp/altune-acquire-test/audio.opus")
	if err != nil {
		t.Fatalf("ValidateDecodable() = %v, want nil for a decode that timed out", err)
	}
	if !strings.Contains(logs.String(), "acquisition.decode_timeout_accepting") {
		t.Errorf("timeout was accepted without a warn record, got logs:\n%s", logs.String())
	}
}

func TestFfprobeProber_ValidateDecodable_WarnsWhenTheDecoderCannotStart(t *testing.T) {
	logs := captureDefaultLogs(t)
	p := NewFfprobeProber("")
	p.ffmpeg = filepath.Join(t.TempDir(), "ffmpeg-absent")

	err := p.ValidateDecodable(context.Background(), "/tmp/altune-acquire-test/audio.opus")
	if err != nil {
		t.Fatalf("ValidateDecodable() = %v, want nil when ffmpeg is missing", err)
	}
	logged := logs.String()
	if !strings.Contains(logged, "acquisition.decoder_unavailable_accepting") || !strings.Contains(logged, `"level":"WARN"`) {
		t.Errorf("fail-open on a missing decoder logged nothing, got logs:\n%s", logged)
	}
}

func TestFfprobeProber_ValidateDecodable_ReportsWhatTheDecoderRefused(t *testing.T) {
	p := fakeFfmpegProber(t, "#!/bin/sh\necho 'Invalid data found when processing input' >&2\nexit 1\n", time.Minute)

	err := p.ValidateDecodable(context.Background(), "/tmp/altune-acquire-test/audio.opus")

	if err == nil {
		t.Fatal("ValidateDecodable() = nil, want an error when the decoder rejects the file")
	}
	if !strings.Contains(err.Error(), "audio stream failed to decode: Invalid data found when processing input") {
		t.Errorf("ValidateDecodable() = %v, want the decoder's own diagnostic", err)
	}
}

func TestFfprobeProber_ValidateDecodable_PropagatesACancelledCaller(t *testing.T) {
	p := fakeFfmpegProber(t, "#!/bin/sh\nsleep 30\n", time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := p.ValidateDecodable(ctx, "/tmp/altune-acquire-test/audio.opus")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ValidateDecodable() = %v, want the caller's cancellation", err)
	}
}
