package ytdlp

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/childprocess"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const cookieCopyDirPattern = domain.TempDirPrefix + "cookies-*"

const canaryTimeout = 60 * time.Second

const soundCloudPreviewDuration = 30.0

const (
	youtubeCanaryName    = string(domain.ProviderYouTube)
	ytMusicCanaryName    = "ytmusic"
	soundCloudCanaryName = string(domain.ProviderSoundCloud)
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

func (s *YouTubeAudioSearcher) Canary(ctx context.Context, source CanarySource) error {
	cookies, err := s.leaseCookieJar(ctx, "canary", source.URL)
	if err != nil {
		return errors.New(redact.LogText(err.Error()))
	}
	defer cookies.cleanup()

	return s.runCanaryProbe(ctx, source, cookies)
}

func (s *YouTubeAudioSearcher) runCanaryProbe(parent context.Context, source CanarySource, cookies cookieLease) error {
	ctx, cancel := context.WithTimeout(parent, s.canaryTimeout)
	defer cancel()
	format := source.Format
	if format == "" {
		format = audioFormatSelector
	}
	args := s.authFlags([]string{
		"--no-warnings", "--simulate", "-f", format, "--print", "%(duration)s", "--", ytDlpURL(source.URL),
	}, cookies.path)

	stdout, stderr, err := childprocess.Run(ctx, ytDlpBinary, args...)
	cookies.settle(parent, err, stderr)
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

func (s *YouTubeAudioSearcher) canaryStopped(parent, run context.Context) error {
	if domain.RunTimedOut(parent, run) {
		return fmt.Errorf("timed out after %s", s.canaryTimeout)
	}
	if err := parent.Err(); err != nil {
		return fmt.Errorf("canary stopped: %w", err)
	}
	return nil
}

func isSoundCloudPreviewDuration(stdout string) bool {
	duration, err := strconv.ParseFloat(strings.TrimSpace(stdout), 64)
	return err == nil && isSoundCloudPreviewLength(duration)
}

func writeTempCopy(contents []byte, pattern string) (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", cookieCopyDirPattern)
	if err != nil {
		return "", func() {}, fmt.Errorf("create the cookie copy dir: %w", err)
	}
	removeDir := func() { _ = os.RemoveAll(dir) }

	dst, err := os.CreateTemp(dir, pattern)
	if err == nil {
		_, err = dst.Write(contents)
		err = errors.Join(err, dst.Close())
	}
	if err != nil {
		removeDir()
		return "", func() {}, err
	}
	return dst.Name(), removeDir, nil
}
