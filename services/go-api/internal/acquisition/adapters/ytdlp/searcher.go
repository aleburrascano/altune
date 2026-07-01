package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"altune/go-api/internal/acquisition/ports"
)

// searchRunner issues a single yt-dlp search for a fully-formed search spec
// (e.g. "ytsearch5:<query>") and returns the parsed candidates. Seamed onto the
// searcher so the dual-engine fan-out is unit-testable without invoking yt-dlp.
type searchRunner func(ctx context.Context, searchSpec string) ([]ports.AudioCandidate, error)

// searchEngines are the yt-dlp search prefixes acquisition fans each query out
// to. YouTube covers the mainstream catalogue; SoundCloud covers the unreleased
// / leaked / underground long tail YouTube does not index. Selection is
// Topic-channel-first, so a SoundCloud candidate only fills a gap (no qualifying
// YouTube Topic match) — it never displaces a good YouTube result.
var searchEngines = []string{"ytsearch5:", "scsearch5:"}

type YtDlpAudioSearcher struct {
	ffmpegLocation string
	cookieFile     string
	jsRuntime      string
	runSearch      searchRunner
}

func NewYtDlpAudioSearcher(ffmpegLocation, cookieFile, jsRuntime string) *YtDlpAudioSearcher {
	s := &YtDlpAudioSearcher{
		ffmpegLocation: ffmpegLocation,
		cookieFile:     cookieFile,
		jsRuntime:      jsRuntime,
	}
	s.runSearch = s.runYtDlpSearch
	return s
}

// Search fans the query out to every search engine, merging the candidates
// (deduped by URL). A single engine failing is tolerated — only the other
// engine's results are kept; acquisition fails the search only if every engine
// errors.
func (s *YtDlpAudioSearcher) Search(ctx context.Context, query string) ([]ports.AudioCandidate, error) {
	seen := make(map[string]bool)
	merged := []ports.AudioCandidate{}
	var firstErr error
	failures := 0

	for _, engine := range searchEngines {
		spec := engine + query
		candidates, err := s.runSearch(ctx, spec)
		if err != nil {
			failures++
			if firstErr == nil {
				firstErr = err
			}
			slog.WarnContext(ctx, "acquisition.engine_search_failed", "spec", spec, "error", err)
			continue
		}
		slog.InfoContext(ctx, "acquisition.engine_search_results", "spec", spec, "candidates", len(candidates))
		for _, c := range candidates {
			if c.URL == "" || seen[c.URL] {
				continue
			}
			seen[c.URL] = true
			merged = append(merged, c)
		}
	}

	if failures == len(searchEngines) {
		return nil, fmt.Errorf("all search engines failed: %w", firstErr)
	}
	return merged, nil
}

// runYtDlpSearch is the real subprocess implementation of searchRunner.
func (s *YtDlpAudioSearcher) runYtDlpSearch(ctx context.Context, searchSpec string) ([]ports.AudioCandidate, error) {
	searchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := []string{
		"--dump-json",
		"--no-download",
		"--flat-playlist",
		"--",
		searchSpec,
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
		"-f", "bestaudio[ext=m4a]/bestaudio",
		"-x",
		"--audio-format", "m4a",
		"--audio-quality", "0",
		"--no-progress",
		"-o", outTemplate,
		"--",
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

	matches, err := filepath.Glob(filepath.Join(outDir, "*.m4a"))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no m4a file produced in %s", outDir)
	}

	// Pick the largest m4a: a single-track download yields one file, but if
	// several appear, the full track is the largest (partial/sidecar files are
	// smaller).
	best, bestSize := "", int64(-1)
	for _, m := range matches {
		info, statErr := os.Stat(m)
		if statErr != nil {
			continue
		}
		if info.Size() > bestSize {
			best, bestSize = m, info.Size()
		}
	}
	if best == "" {
		return "", fmt.Errorf("stat downloaded files in %s", outDir)
	}
	const minFileSize = 10 * 1024 // 10KB
	if bestSize < minFileSize {
		return "", fmt.Errorf("downloaded file too small (%d bytes), likely corrupt", bestSize)
	}

	return best, nil
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
