package ytdlp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/execcmd"
	sharedytdlp "altune/go-api/internal/shared/ytdlp"
)

type searchRunner func(ctx context.Context, searchSpec string) ([]ports.AudioCandidate, error)

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

func (s *YtDlpAudioSearcher) Search(ctx context.Context, query string) ([]ports.AudioCandidate, error) {
	return ports.CollectCandidates(
		len(searchEngines),
		func(i int) ([]ports.AudioCandidate, error) {
			return s.runSearch(ctx, searchEngines[i]+query)
		},
		func(i int, candidates []ports.AudioCandidate) {
			slog.InfoContext(ctx, "acquisition.engine_search_results",
				"spec", searchEngines[i]+query, "candidates", len(candidates))
		},
		func(i int, err error) {
			slog.WarnContext(ctx, "acquisition.engine_search_failed",
				"spec", searchEngines[i]+query, "error", err)
		},
		func(firstErr error) error {
			return fmt.Errorf("all search engines failed: %w", firstErr)
		},
	)
}

func (s *YtDlpAudioSearcher) prependAuthFlags(args []string) []string {
	if s.jsRuntime != "" {
		args = append([]string{"--js-runtimes", s.jsRuntime, "--remote-components", "ejs:github"}, args...)
	}
	if s.cookieFile != "" {
		args = append([]string{"--cookies", s.cookieFile}, args...)
	}
	return args
}

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
	args = s.prependAuthFlags(args)

	lines, stderr, err := sharedytdlp.DumpJSON(searchCtx, args)
	if err != nil {
		return nil, fmt.Errorf("yt-dlp search: %w (stderr: %s)", err, stderr)
	}

	var candidates []ports.AudioCandidate
	for _, line := range lines {
		var entry ytDlpEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		candidates = append(candidates, ports.AudioCandidate{
			Title:      entry.Title,
			Duration:   entry.Duration,
			URL:        entry.WebpageURL,
			Channel:    entry.Channel,
			Categories: entry.Categories,
			ViewCount:  entry.ViewCount,
		})
	}

	return candidates, nil
}

func (s *YtDlpAudioSearcher) Download(ctx context.Context, url string, outDir string) (string, error) {
	outTemplate := filepath.Join(outDir, "%(title)s.%(ext)s")
	args := []string{
		"-f", "bestaudio",
		"-x",
		"--audio-format", "mp3",
		"--audio-quality", "0",
		"--no-progress",
		"-o", outTemplate,
		"--",
		url,
	}

	if s.ffmpegLocation != "" {
		args = append([]string{"--ffmpeg-location", s.ffmpegLocation}, args...)
	}
	args = s.prependAuthFlags(args)

	_, stderr, err := execcmd.RunWithTimeout(ctx, 5*time.Minute, "yt-dlp", args...)
	if err != nil {
		return "", fmt.Errorf("yt-dlp download: %w (stderr: %s)", err, stderr)
	}

	matches, err := filepath.Glob(filepath.Join(outDir, "*.mp3"))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no mp3 file produced in %s", outDir)
	}

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
	const minFileSize = 10 * 1024
	if bestSize < minFileSize {
		return "", fmt.Errorf("downloaded file too small (%d bytes), likely corrupt", bestSize)
	}

	return best, nil
}

type ytDlpEntry struct {
	Title      string   `json:"title"`
	Duration   float64  `json:"duration"`
	WebpageURL string   `json:"webpage_url"`
	Channel    string   `json:"channel"`
	Categories []string `json:"categories"`
	ViewCount  int64    `json:"view_count"`
}
