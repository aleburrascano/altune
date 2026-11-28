package ytdlp_test

import (
	"altune/go-api/internal/download/internal/adapters/ytdlp"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/logging"
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

const decodedFile = "/tmp/altune-acquire-test/audio.opus"

func fakeFfmpegDir(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func refusingFfmpegDir(t *testing.T, stderr string) string {
	t.Helper()
	return fakeFfmpegDir(t, "#!/bin/sh\nprintf '%s\\n' '"+stderr+"' >&2\nexit 1\n")
}

type skipRecorder interface{ RecordVerifySkip(gate string) }

func newProber(t testing.TB, ffmpegLocation string, skips skipRecorder, opts ...func(*ytdlp.FfprobeProber)) *ytdlp.FfprobeProber {
	t.Helper()
	p, err := ytdlp.NewFfprobeProber(ffmpegLocation, skips, opts...)
	if err != nil {
		t.Fatalf("NewFfprobeProber: %v", err)
	}
	return p
}

func TestNewFfprobeProber_RefusesAMissingSkipRecorder(t *testing.T) {
	p, err := ytdlp.NewFfprobeProber(t.TempDir(), nil)

	if !errors.Is(err, ytdlp.ErrSkipRecorderRequired) || p != nil {
		t.Fatalf("got (%v, %v), want a nil prober and ErrSkipRecorderRequired", p, err)
	}
}

func missingFfmpeg(t *testing.T) string {
	t.Helper()
	withoutAnyBinaries(t)
	return t.TempDir()
}

func TestFfprobeProber_ValidateDecodable_AcceptsADecodeThatOutranItsDeadline(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	p := newProber(t, fakeFfmpegDir(t, "#!/bin/sh\nsleep 30\n"), ports.NoopVerifySkipRecorder(), ytdlp.WithDecodeTimeout(100*time.Millisecond))

	err := p.ValidateDecodable(context.Background(), decodedFile)
	if err != nil {
		t.Fatalf("ValidateDecodable() = %v, want nil for a decode that timed out", err)
	}
	if !strings.Contains(logs.String(), "acquisition.decode_timeout_accepting") {
		t.Errorf("timeout was accepted without a warn record, got logs:\n%s", logs.String())
	}
}

func TestFfprobeProber_ValidateDecodable_WarnsWhenTheDecoderCannotStart(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	p := newProber(t, missingFfmpeg(t), ports.NoopVerifySkipRecorder())

	err := p.ValidateDecodable(context.Background(), decodedFile)
	if err != nil {
		t.Fatalf("ValidateDecodable() = %v, want nil when ffmpeg is missing", err)
	}
	logged := logs.String()
	if !strings.Contains(logged, "acquisition.decoder_unavailable_accepting") || !strings.Contains(logged, `"level":"WARN"`) {
		t.Errorf("fail-open on a missing decoder logged nothing, got logs:\n%s", logged)
	}
}

func unrunnableFfmpegOutsideTheTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte("not a program"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", t.TempDir())
	return dir
}

func TestFfprobeProber_ValidateDecodable_MasksTheHostBinPathWhenTheDecoderCannotStart(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	binDir := unrunnableFfmpegOutsideTheTempDir(t)
	p := newProber(t, binDir, ports.NoopVerifySkipRecorder())

	err := p.ValidateDecodable(context.Background(), decodedFile)
	if err != nil {
		t.Fatalf("ValidateDecodable() = %v, want nil when ffmpeg cannot start", err)
	}
	logged := logs.String()
	if strings.Contains(logged, binDir) {
		t.Errorf("decoder-unavailable log leaked the host bin path, got logs:\n%s", logged)
	}
	if !strings.Contains(logged, "acquisition.decoder_unavailable_accepting") {
		t.Errorf("fail-open on a missing decoder logged nothing, got logs:\n%s", logged)
	}
}

func TestFfprobeProber_ValidateDecodable_ReportsWhatTheDecoderRefused(t *testing.T) {
	p := newProber(t, refusingFfmpegDir(t, "Invalid data found when processing input"), ports.NoopVerifySkipRecorder())

	err := p.ValidateDecodable(context.Background(), decodedFile)

	if err == nil {
		t.Fatal("ValidateDecodable() = nil, want an error when the decoder rejects the file")
	}
	if !strings.Contains(err.Error(), "audio stream failed to decode: Invalid data found when processing input") {
		t.Errorf("ValidateDecodable() = %v, want the decoder's own diagnostic", err)
	}
}

func TestFfprobeProber_ValidateDecodable_PropagatesACancelledCaller(t *testing.T) {
	started := filepath.Join(t.TempDir(), "started")
	p := newProber(t, fakeFfmpegDir(t, "#!/bin/sh\ntouch '"+started+"'\nsleep 30\n"), ports.NoopVerifySkipRecorder())
	ctx := cancelOnceFileExists(t, started)

	err := p.ValidateDecodable(ctx, decodedFile)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ValidateDecodable() = %v, want the caller's cancellation", err)
	}
}

type gateRecorder struct{ gates []string }

func (r *gateRecorder) RecordVerifySkip(gate string) { r.gates = append(r.gates, gate) }

func TestFfprobeProber_ValidateDecodable_RecordsADecodeTimeoutSkip(t *testing.T) {
	rec := &gateRecorder{}
	p := newProber(t, fakeFfmpegDir(t, "#!/bin/sh\nsleep 30\n"), rec, ytdlp.WithDecodeTimeout(100*time.Millisecond))

	if err := p.ValidateDecodable(context.Background(), decodedFile); err != nil {
		t.Fatalf("ValidateDecodable() = %v, want nil", err)
	}
	if len(rec.gates) != 1 || rec.gates[0] != "decode_timeout" {
		t.Errorf("recorded gates = %v, want [decode_timeout]", rec.gates)
	}
}

func TestFfprobeProber_ValidateDecodable_RecordsADecoderUnavailableSkip(t *testing.T) {
	rec := &gateRecorder{}
	p := newProber(t, missingFfmpeg(t), rec)

	if err := p.ValidateDecodable(context.Background(), decodedFile); err != nil {
		t.Fatalf("ValidateDecodable() = %v, want nil", err)
	}
	if len(rec.gates) != 1 || rec.gates[0] != "decoder_unavailable" {
		t.Errorf("recorded gates = %v, want [decoder_unavailable]", rec.gates)
	}
}

func decoderDiagnostic(t *testing.T, stderr string) string {
	t.Helper()
	err := newProber(t, refusingFfmpegDir(t, stderr), ports.NoopVerifySkipRecorder()).ValidateDecodable(context.Background(), decodedFile)
	if err == nil {
		t.Fatal("ValidateDecodable() = nil, want the decoder's refusal")
	}
	diagnostic, ok := strings.CutPrefix(err.Error(), "audio stream failed to decode: ")
	if !ok {
		t.Fatalf("ValidateDecodable() = %v, want the decode refusal", err)
	}
	return diagnostic
}

func TestFfprobeProber_ValidateDecodable_CapsTheDiagnosticOnARuneBoundary(t *testing.T) {
	got := decoderDiagnostic(t, strings.Repeat("a", 199)+"é"+"tail")
	if !utf8.ValidString(got) {
		t.Fatalf("invalid UTF-8: %q", got)
	}
	if got != strings.Repeat("a", 199) {
		t.Fatalf("got len %d", len(got))
	}
}

func TestFfprobeProber_ValidateDecodable_LeavesAShortDiagnosticAndCapsLongASCII(t *testing.T) {
	long := strings.Repeat("b", 250)
	if got := decoderDiagnostic(t, long); got != long[:200] {
		t.Fatalf("ascii cap changed: len %d", len(got))
	}
	if got := decoderDiagnostic(t, "héllo\nsecond line"); got != "héllo" {
		t.Fatalf("short changed: %q", got)
	}
}

func TestFfprobeProber_ValidateDecodable_ANonPositiveTimeoutKeepsTheDefault(t *testing.T) {
	rec := &gateRecorder{}
	p := newProber(t, fakeFfmpegDir(t, "#!/bin/sh\nexit 0\n"), rec, ytdlp.WithDecodeTimeout(0))

	if err := p.ValidateDecodable(context.Background(), decodedFile); err != nil {
		t.Fatalf("ValidateDecodable() = %v, want nil", err)
	}
	if len(rec.gates) != 0 {
		t.Errorf("recorded gates = %v, want none: a zero timeout must leave the default in place, not skip the decode", rec.gates)
	}
}
