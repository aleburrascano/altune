package ytdlp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// FfprobeProber reads an audio file's actual duration via ffprobe. It implements
// acquisition's AudioProber port, used to verify a downloaded file is the right
// recording before it is stored.
type FfprobeProber struct {
	binary string
}

// NewFfprobeProber resolves the ffprobe binary near the configured ffmpeg
// location (yt-dlp's --ffmpeg-location dir), falling back to "ffprobe" on PATH.
func NewFfprobeProber(ffmpegLocation string) *FfprobeProber {
	binary := "ffprobe"
	if ffmpegLocation != "" {
		name := "ffprobe"
		if runtime.GOOS == "windows" {
			name = "ffprobe.exe"
		}
		candidate := filepath.Join(ffmpegLocation, name)
		if _, err := os.Stat(candidate); err == nil {
			binary = candidate
		}
	}
	return &FfprobeProber{binary: binary}
}

func (p *FfprobeProber) ProbeDuration(ctx context.Context, filePath string) (float64, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, p.binary,
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
