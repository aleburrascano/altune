package ytdlp

import (
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

	// defaultDecodeTimeout bounds a full-file decode, which is I/O bound and
	// runs on the acquisition worker's budget. Tests shorten it per prober.
	defaultDecodeTimeout = 90 * time.Second
)

type FfprobeProber struct {
	ffprobe       string
	ffmpeg        string
	decodeTimeout time.Duration
}

func NewFfprobeProber(ffmpegLocation string) *FfprobeProber {
	return &FfprobeProber{
		ffprobe:       binpath.Resolve("ffprobe", ffmpegLocation),
		ffmpeg:        binpath.Resolve("ffmpeg", ffmpegLocation),
		decodeTimeout: defaultDecodeTimeout,
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

// ValidateDecodable returns an error only for what the decoder itself refused.
// A decode killed by its own deadline, and a decoder that never started, are
// silence rather than a verdict, so both are accepted with a warn: treating
// them as "undecodable" condemns a good download the caller never retries.
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
		return nil
	case errors.As(err, &exitErr):
		return fmt.Errorf("audio stream failed to decode: %s", firstLine(stderr))
	default:
		slog.WarnContext(ctx, "acquisition.decoder_unavailable_accepting",
			"file", filePath, "error", redact.Secrets(err.Error()))
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
