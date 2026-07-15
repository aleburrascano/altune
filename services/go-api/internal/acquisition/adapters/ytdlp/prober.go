package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// FfprobeProber inspects a downloaded audio file with ffmpeg tooling: ffprobe for
// duration, ffmpeg for decode validation. It implements acquisition's AudioProber
// port, used to verify a downloaded file before it is stored.
type FfprobeProber struct {
	ffprobe string
	ffmpeg  string
}

// NewFfprobeProber resolves the ffprobe/ffmpeg binaries near the configured ffmpeg
// location (yt-dlp's --ffmpeg-location dir), falling back to the bare name on PATH.
func NewFfprobeProber(ffmpegLocation string) *FfprobeProber {
	return &FfprobeProber{
		ffprobe: resolveBinary("ffprobe", ffmpegLocation),
		ffmpeg:  resolveBinary("ffmpeg", ffmpegLocation),
	}
}

func resolveBinary(name, ffmpegLocation string) string {
	if ffmpegLocation != "" {
		candidate := name
		if runtime.GOOS == "windows" {
			candidate = name + ".exe"
		}
		full := filepath.Join(ffmpegLocation, candidate)
		if _, err := os.Stat(full); err == nil {
			return full
		}
	}
	return name
}

func (p *FfprobeProber) ProbeDuration(ctx context.Context, filePath string) (float64, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, p.ffprobe,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		filePath,
	)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}

	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &probe); err != nil {
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

// ValidateDecodable decodes the whole stream to null and reports an error if the
// decoder rejects the audio. ffprobe/ProbeDuration only reads container metadata,
// so a file with a valid header but corrupt samples passes duration verification
// yet fails here — the exact defect that shipped undecodable m4a files. A non-zero
// ffmpeg exit means the samples don't decode. If ffmpeg itself cannot be run
// (missing binary, timeout), validation is skipped rather than blocking acquisition
// on an unavailable validator — mirroring ProbeDuration's fail-open stance.
func (p *FfprobeProber) ValidateDecodable(ctx context.Context, filePath string) error {
	decodeCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(decodeCtx, p.ffmpeg,
		"-v", "error",
		"-i", filePath,
		"-f", "null",
		"-",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Errorf("audio stream failed to decode: %s", firstLine(stderr.String()))
	}
	return nil
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
