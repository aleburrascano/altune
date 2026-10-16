package ytdlp

import (
	"altune/go-api/internal/acquisition/ports"
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

const cookieCopyDirPattern = ports.TempDirPrefix + "cookies-*"

const canaryTimeout = 60 * time.Second

const soundCloudPreviewDuration = 30.0

const (
	youtubeCanaryName    = string(ports.ProviderYouTube)
	ytMusicCanaryName    = "ytmusic"
	soundCloudCanaryName = string(ports.ProviderSoundCloud)
)

type CanarySource struct {
	Name   string
	URL    string
	Format string
}

var audioOnlyFormat, _, _ = strings.Cut(audioFormatSelector, "/best[")

var (
	YouTubeCanary    = CanarySource{Name: youtubeCanaryName, URL: "https://www.youtube.com/watch?v=jNQXAC9IVRw"}
	YTMusicCanary    = CanarySource{Name: ytMusicCanaryName, URL: "https://music.youtube.com/watch?v=dQw4w9WgXcQ", Format: audioOnlyFormat}
	SoundCloudCanary = CanarySource{Name: soundCloudCanaryName, URL: "https://api.soundcloud.com/tracks/soundcloud%3Atracks%3A157015344"}
)

func (s *YtDlpAudioSearcher) Canary(ctx context.Context, source CanarySource) error {
	cookieFile, cleanup, err := s.cookieJarCopy("acquisition-canary-cookies-*.txt")
	if err != nil {
		return errors.New(redact.LogText(err.Error()))
	}
	defer cleanup()

	canaryCtx, cancel := context.WithTimeout(ctx, s.canaryTimeout)
	defer cancel()

	return s.runCanaryProbe(ctx, canaryCtx, source, cookieFile)
}

func (s *YtDlpAudioSearcher) runCanaryProbe(parent, ctx context.Context, source CanarySource, cookieFile string) error {
	format := source.Format
	if format == "" {
		format = audioFormatSelector
	}
	args := s.authFlags([]string{
		"--no-warnings", "--simulate", "-f", format, "--print", "%(duration)s", "--", ytDlpURL(source.URL),
	}, cookieFile)

	stdout, stderr, err := execcmd.Run(ctx, s.binary, args...)
	if stopped := s.canaryStopped(parent, ctx); stopped != nil {
		return stopped
	}
	if err != nil {
		return errors.New(redact.LogText(firstLine(stderr)))
	}
	if source.Name == soundCloudCanaryName && isSoundCloudPreviewDuration(stdout) {
		return errors.New("preview only")
	}
	return nil
}

func (s *YtDlpAudioSearcher) canaryStopped(parent, run context.Context) error {
	if ports.RunTimedOut(parent, run) {
		return fmt.Errorf("timed out after %s", s.canaryTimeout)
	}
	return parent.Err()
}

func isSoundCloudPreviewDuration(stdout string) bool {
	duration, err := strconv.ParseFloat(strings.TrimSpace(stdout), 64)
	return err == nil && isSoundCloudPreviewLength(duration)
}

func (s *YtDlpAudioSearcher) cookieJarCopy(pattern string) (path string, cleanup func(), err error) {
	if s.cookieFile == "" {
		return "", func() {}, nil
	}
	return CopyToTempFile(s.cookieFile, pattern)
}

func CopyToTempFile(sourcePath, pattern string) (path string, cleanup func(), err error) {
	source, err := os.Open(sourcePath)
	if err != nil {
		return "", func() {}, err
	}
	defer func() { _ = source.Close() }()

	dir, err := os.MkdirTemp("", cookieCopyDirPattern)
	if err != nil {
		return "", func() {}, err
	}
	removeDir := func() { _ = os.RemoveAll(dir) }

	dst, err := os.CreateTemp(dir, pattern)
	if err != nil {
		removeDir()
		return "", func() {}, err
	}
	defer func() { _ = dst.Close() }()

	if _, err := io.Copy(dst, source); err != nil {
		removeDir()
		return "", func() {}, err
	}
	return dst.Name(), removeDir, nil
}
