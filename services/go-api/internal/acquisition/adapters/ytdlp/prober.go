package ytdlp

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/execcmd"
	"altune/go-api/internal/shared/redact"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	probeTimeout = 30 * time.Second

	defaultDecodeTimeout = 90 * time.Second
)

type FfprobeProber struct {
	ffprobe       string
	ffmpeg        string
	decodeTimeout time.Duration
	skips         ports.VerifySkipRecorder
}

func WithSkipRecorder(r ports.VerifySkipRecorder) func(*FfprobeProber) {
	return func(p *FfprobeProber) { p.skips = r }
}

func NewFfprobeProber(ffmpegLocation string, opts ...func(*FfprobeProber)) *FfprobeProber {
	p := &FfprobeProber{
		ffprobe:       binpath.Resolve("ffprobe", ffmpegLocation),
		ffmpeg:        binpath.Resolve("ffmpeg", ffmpegLocation),
		decodeTimeout: defaultDecodeTimeout,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *FfprobeProber) recordSkip(gate string) {
	if p.skips != nil {
		p.skips.RecordVerifySkip(gate)
	}
}

func (p *FfprobeProber) Available() (ffprobe bool, ffmpeg bool) {
	return binpath.Runnable(p.ffprobe), binpath.Runnable(p.ffmpeg)
}

func (p *FfprobeProber) ProbeDuration(ctx context.Context, filePath string) (float64, error) {
	output, _, err := execcmd.RunWithTimeout(ctx, probeTimeout, p.ffprobe,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		filePath,
	)
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}

	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal([]byte(output), &probe); err != nil {
		return 0, fmt.Errorf("parse ffprobe output: %w", err)
	}

	duration, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", probe.Format.Duration, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("invalid duration: %.2f", duration)
	}
	return duration, nil
}

func (p *FfprobeProber) ValidateDecodable(ctx context.Context, filePath string) error {
	decodeCtx, cancel := context.WithTimeout(ctx, p.decodeTimeout)
	defer cancel()

	_, stderr, err := execcmd.Run(decodeCtx, p.ffmpeg,
		"-v", "error",
		"-i", filePath,
		"-f", "null",
		"-",
	)
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(decodeCtx.Err(), context.DeadlineExceeded):
		slog.WarnContext(ctx, "acquisition.decode_timeout_accepting",
			"file", filePath, "timeout", p.decodeTimeout.String())
		p.recordSkip(ports.SkipDecodeTimeout)
		return nil
	case errors.As(err, &exitErr):
		return fmt.Errorf("audio stream failed to decode: %s", firstLine(stderr))
	default:
		slog.WarnContext(ctx, "acquisition.decoder_unavailable_accepting",
			"file", filePath, "error", redact.LogError(err))
		p.recordSkip(ports.SkipDecoderUnavailable)
		return nil
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	if s == "" {
		return "decoder produced no diagnostic output"
	}
	return s
}
