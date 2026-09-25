package ytdlp

import (
	"altune/go-api/internal/shared/execcmd"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const canaryTimeout = 60 * time.Second

const soundCloudPreviewDuration = 30.0

const (
	youtubeCanaryName    = "youtube"
	ytMusicCanaryName    = "ytmusic"
	soundCloudCanaryName = "soundcloud"
)

type CanarySource struct {
	Name string
	URL  string
}

var (
	YouTubeCanary    = CanarySource{Name: youtubeCanaryName, URL: "https://www.youtube.com/watch?v=jNQXAC9IVRw"}
	YTMusicCanary    = CanarySource{Name: ytMusicCanaryName, URL: "https://music.youtube.com/watch?v=jNQXAC9IVRw"}
	SoundCloudCanary = CanarySource{Name: soundCloudCanaryName, URL: "https://api.soundcloud.com/tracks/soundcloud%3Atracks%3A157015344"}
)

func (s *YtDlpAudioSearcher) Canary(ctx context.Context, source CanarySource) error {
	cookieFile, cleanup, err := s.canaryCookieFile()
	if err != nil {
		return errors.New(redact.LogText(err.Error()))
	}
	defer cleanup()

	canaryCtx, cancel := context.WithTimeout(ctx, s.canaryTimeout)
	defer cancel()

	return s.runCanaryProbe(canaryCtx, source, cookieFile)
}

func (s *YtDlpAudioSearcher) runCanaryProbe(ctx context.Context, source CanarySource, cookieFile string) error {
	args := s.authFlags([]string{
		"--no-warnings", "--simulate", "-f", audioFormatSelector, "--print", "%(duration)s", "--", source.URL,
	}, cookieFile)

	stdout, stderr, err := execcmd.Run(ctx, s.binary, args...)
	if ctx.Err() != nil {
		return fmt.Errorf("timed out after %s", s.canaryTimeout)
	}
	if err != nil {
		return errors.New(redact.LogText(firstLine(stderr)))
	}
	if source.Name == soundCloudCanaryName && isSoundCloudPreviewDuration(stdout) {
		return errors.New("preview only")
	}
	return nil
}

func isSoundCloudPreviewDuration(stdout string) bool {
	duration, err := strconv.ParseFloat(strings.TrimSpace(stdout), 64)
	return err == nil && duration == soundCloudPreviewDuration
}

func (s *YtDlpAudioSearcher) canaryCookieFile() (path string, cleanup func(), err error) {
	if s.cookieFile == "" {
		return "", func() {}, nil
	}
	return copyToTempFile(s.cookieFile, "acquisition-canary-cookies-*.txt")
}

func copyToTempFile(sourcePath, pattern string) (path string, cleanup func(), err error) {
	source, err := os.Open(sourcePath)
	if err != nil {
		return "", func() {}, err
	}
	defer func() { _ = source.Close() }()

	dst, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", func() {}, err
	}
	defer func() { _ = dst.Close() }()

	if _, err := io.Copy(dst, source); err != nil {
		_ = os.Remove(dst.Name())
		return "", func() {}, err
	}
	return dst.Name(), func() { _ = os.Remove(dst.Name()) }, nil
}
