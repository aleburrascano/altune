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
	"unicode/utf8"
)

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

func TestFfprobeProber_ValidateDecodable_MasksTheHostBinPathWhenTheDecoderCannotStart(t *testing.T) {
	logs := captureDefaultLogs(t)
	p := NewFfprobeProber("")
	p.ffmpeg = "/opt/altune/vendor-bin/ffmpeg"

	err := p.ValidateDecodable(context.Background(), "/tmp/altune-acquire-test/audio.opus")
	if err != nil {
		t.Fatalf("ValidateDecodable() = %v, want nil when ffmpeg is missing", err)
	}
	logged := logs.String()
	if strings.Contains(logged, "/opt/altune/vendor-bin/ffmpeg") {
		t.Errorf("decoder-unavailable log leaked the host bin path, got logs:\n%s", logged)
	}
	if !strings.Contains(logged, "acquisition.decoder_unavailable_accepting") {
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

type gateRecorder struct{ gates []string }

func (r *gateRecorder) RecordVerifySkip(gate string) { r.gates = append(r.gates, gate) }

func TestFfprobeProber_ValidateDecodable_RecordsADecodeTimeoutSkip(t *testing.T) {
	rec := &gateRecorder{}
	p := fakeFfmpegProber(t, "#!/bin/sh\nsleep 30\n", 100*time.Millisecond)
	WithSkipRecorder(rec)(p)

	if err := p.ValidateDecodable(context.Background(), "/tmp/altune-acquire-test/audio.opus"); err != nil {
		t.Fatalf("ValidateDecodable() = %v, want nil", err)
	}
	if len(rec.gates) != 1 || rec.gates[0] != "decode_timeout" {
		t.Errorf("recorded gates = %v, want [decode_timeout]", rec.gates)
	}
}

func TestFfprobeProber_ValidateDecodable_RecordsADecoderUnavailableSkip(t *testing.T) {
	rec := &gateRecorder{}
	p := NewFfprobeProber("", WithSkipRecorder(rec))
	p.ffmpeg = filepath.Join(t.TempDir(), "ffmpeg-absent")

	if err := p.ValidateDecodable(context.Background(), "/tmp/altune-acquire-test/audio.opus"); err != nil {
		t.Fatalf("ValidateDecodable() = %v, want nil", err)
	}
	if len(rec.gates) != 1 || rec.gates[0] != "decoder_unavailable" {
		t.Errorf("recorded gates = %v, want [decoder_unavailable]", rec.gates)
	}
}

func TestFirstLineKeepsValidUTF8WhenRuneCrossesCap(t *testing.T) {
	in := strings.Repeat("a", 199) + "é" + "tail"
	got := firstLine(in)
	if !utf8.ValidString(got) {
		t.Fatalf("invalid UTF-8: %q", got)
	}
	if got != strings.Repeat("a", 199) {
		t.Fatalf("got len %d", len(got))
	}
}

func TestFirstLineLeavesShortAndASCIIUnchanged(t *testing.T) {
	in := strings.Repeat("b", 250)
	if got := firstLine(in); got != in[:200] {
		t.Fatalf("ascii cap changed: len %d", len(got))
	}
	if got := firstLine("héllo"); got != "héllo" {
		t.Fatalf("short changed: %q", got)
	}
}
