package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"altune/go-api/internal/catalog/ports"
)

type YtDlpAudioSearcher struct {
	ffmpegLocation string
	cookieFile     string
	jsRuntime      string
}

func NewYtDlpAudioSearcher(ffmpegLocation, cookieFile, jsRuntime string) *YtDlpAudioSearcher {
	return &YtDlpAudioSearcher{
		ffmpegLocation: ffmpegLocation,
		cookieFile:     cookieFile,
		jsRuntime:      jsRuntime,
	}
}

func (s *YtDlpAudioSearcher) Search(ctx context.Context, query string) ([]ports.AudioCandidate, error) {
	searchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := []string{
		"--dump-json",
		"--no-download",
		"--flat-playlist",
		fmt.Sprintf("ytsearch5:%s", query),
	}
	if s.jsRuntime != "" {
		args = append([]string{"--js-runtimes", s.jsRuntime, "--remote-components", "ejs:github"}, args...)
	}
	if s.cookieFile != "" {
		args = append([]string{"--cookies", s.cookieFile}, args...)
	}

	cmd := exec.CommandContext(searchCtx, "yt-dlp", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("yt-dlp search: %w (stderr: %s)", err, stderr.String())
	}

	var candidates []ports.AudioCandidate
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var entry ytDlpEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		candidates = append(candidates, ports.AudioCandidate{
			Title:         entry.Title,
			Artist:        entry.Uploader,
			DurationSecs:  entry.Duration,
			URL:           entry.WebpageURL,
			Channel:       entry.Channel,
			Categories:    entry.Categories,
			ViewCount:     entry.ViewCount,
			FollowerCount: entry.FollowerCount,
		})
	}

	return candidates, nil
}

func (s *YtDlpAudioSearcher) Download(ctx context.Context, url string, outDir string) (string, error) {
	downloadCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	outTemplate := filepath.Join(outDir, "%(title)s.%(ext)s")
	args := []string{
		"-f", "bestaudio",
		"-x",
		"--audio-format", "mp3",
		"--audio-quality", "0",
		"--no-progress",
		"-o", outTemplate,
		url,
	}

	if s.jsRuntime != "" {
		args = append([]string{"--js-runtimes", s.jsRuntime, "--remote-components", "ejs:github"}, args...)
	}
	if s.ffmpegLocation != "" {
		args = append([]string{"--ffmpeg-location", s.ffmpegLocation}, args...)
	}
	if s.cookieFile != "" {
		args = append([]string{"--cookies", s.cookieFile}, args...)
	}

	cmd := exec.CommandContext(downloadCtx, "yt-dlp", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("yt-dlp download: %w (stderr: %s)", err, stderr.String())
	}

	matches, err := filepath.Glob(filepath.Join(outDir, "*.mp3"))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no mp3 file produced in %s", outDir)
	}

	info, err := os.Stat(matches[0])
	if err != nil {
		return "", fmt.Errorf("stat downloaded file: %w", err)
	}
	const minFileSize = 10 * 1024 // 10KB
	if info.Size() < minFileSize {
		return "", fmt.Errorf("downloaded file too small (%d bytes), likely corrupt", info.Size())
	}

	return matches[0], nil
}

type ytDlpEntry struct {
	Title         string   `json:"title"`
	Uploader      string   `json:"uploader"`
	Duration      float64  `json:"duration"`
	WebpageURL    string   `json:"webpage_url"`
	Channel       string   `json:"channel"`
	Categories    []string `json:"categories"`
	ViewCount     int64    `json:"view_count"`
	FollowerCount int64    `json:"channel_follower_count"`
}
