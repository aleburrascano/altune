package main

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/execcmd"
	"context"
	"fmt"
	"strings"
)

type mediaTools interface {
	ReadTags(ctx context.Context, path string) (ports.TrackTags, error)
	AudioDigest(ctx context.Context, path string) (string, error)
}

type ffmpegTools struct {
	ffprobe string
	ffmpeg  string
}

func (t ffmpegTools) ReadTags(ctx context.Context, path string) (ports.TrackTags, error) {
	stdout, stderr, err := execcmd.Run(ctx, t.ffprobe,
		"-v", "error", "-print_format", "json", "-show_entries", "format_tags", "-i", path)
	if err != nil {
		return ports.TrackTags{}, fmt.Errorf("ffprobe: %w (stderr: %s)", err, strings.TrimSpace(stderr))
	}
	return parseProbeTags([]byte(stdout))
}

func (t ffmpegTools) AudioDigest(ctx context.Context, path string) (string, error) {
	stdout, stderr, err := execcmd.Run(ctx, t.ffmpeg,
		"-v", "error", "-i", path, "-map", "0:a:0", "-c", "copy", "-f", "md5", "-")
	if err != nil {
		return "", fmt.Errorf("ffmpeg audio digest: %w (stderr: %s)", err, strings.TrimSpace(stderr))
	}
	digest, found := strings.CutPrefix(strings.TrimSpace(stdout), "MD5=")
	if !found || digest == "" {
		return "", fmt.Errorf("ffmpeg audio digest: unexpected output %q", stdout)
	}
	return digest, nil
}
