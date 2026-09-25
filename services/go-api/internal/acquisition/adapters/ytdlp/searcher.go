package ytdlp

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/execcmd"
	"altune/go-api/internal/shared/redact"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	sharedytdlp "altune/go-api/internal/shared/ytdlp"
)

const (
	searchTimeout   = 30 * time.Second
	downloadTimeout = 5 * time.Minute
)

// maxSourceFileSize caps the media yt-dlp will pull before extraction. A single
// track cannot approach it, so anything that does is a mix or a full set that
// would burn the job's budget transcoding and fill /tmp for every concurrent
// worker. yt-dlp's own flag is what stops it, before the bytes are spent.
const maxSourceFileSize = "200M"

type searchRunner func(ctx context.Context, searchSpec string) ([]ports.AudioCandidate, error)

var searchEngines = []string{"ytsearch5:", "scsearch5:"}

type YtDlpAudioSearcher struct {
	ffmpegLocation  string
	cookieFile      string
	jsRuntime       string
	binary          string
	runSearch       searchRunner
	searchTimeout   time.Duration
	downloadTimeout time.Duration
}

func NewYtDlpAudioSearcher(ffmpegLocation, cookieFile, jsRuntime string) *YtDlpAudioSearcher {
	s := &YtDlpAudioSearcher{
		ffmpegLocation:  ffmpegLocation,
		cookieFile:      cookieFile,
		jsRuntime:       jsRuntime,
		binary:          "yt-dlp",
		searchTimeout:   searchTimeout,
		downloadTimeout: downloadTimeout,
	}
	s.runSearch = s.runYtDlpSearch
	return s
}

// Available reports whether the yt-dlp binary is runnable, mirroring the
// ffprobe/ffmpeg (prober) and fpcalc (identifier) availability probes. Surfacing
// this at wiring time turns a missing yt-dlp into a startup verification warning
// rather than a generic error deep in a background acquisition goroutine.
func (s *YtDlpAudioSearcher) Available() bool {
	return binpath.Runnable(s.binary)
}

// classifiedFailure marks a yt-dlp run that failed for a reason carrying no
// evidence about the track — a throttle, an outage, a dead network, or a yt-dlp
// that is not installed — so the pipeline reports it as an unavailable source
// instead of a track that does not exist.
func (s *YtDlpAudioSearcher) classifiedFailure(err error, stderr string, timedOut bool) error {
	if s.Available() && !timedOut && !ports.OutputShowsSourceUnavailable(stderr) {
		return err
	}
	return &ports.SourceUnavailableError{Source: SourceName, Err: err}
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
				"spec", searchEngines[i]+query, "error", redact.LogError(err))
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
	searchCtx, cancel := context.WithTimeout(ctx, s.searchTimeout)
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
		return nil, s.classifiedFailure(fmt.Errorf("yt-dlp search: %w (stderr: %s)", err, stderr), stderr, ports.RunTimedOut(ctx, searchCtx))
	}

	candidates, skipped := candidatesFromEntryLines(lines)
	if len(lines) > 0 && len(candidates) == 0 {
		return nil, fmt.Errorf("yt-dlp search: %d lines, 0 parsable", len(lines))
	}
	if skipped > 0 {
		slog.WarnContext(ctx, "acquisition.search_lines_skipped",
			"spec", searchSpec, "lines", len(lines), "skipped", skipped)
	}

	return candidates, nil
}

// candidatesFromEntryLines maps yt-dlp NDJSON lines to candidates, skipping any
// line that does not yield an entry with a URL (a URL-less candidate is dropped
// by the dedupe downstream anyway). The skipped count is what lets the caller
// tell a drifted output format from a genuinely empty search.
func candidatesFromEntryLines(lines [][]byte) (candidates []ports.AudioCandidate, skipped int) {
	for _, line := range lines {
		var entry ytDlpEntry
		if err := json.Unmarshal(line, &entry); err != nil || entry.WebpageURL == "" {
			skipped++
			continue
		}
		candidates = append(candidates, entry.candidate())
	}
	return candidates, skipped
}

func (s *YtDlpAudioSearcher) Download(ctx context.Context, url string, outDir string) (string, error) {
	outTemplate := filepath.Join(outDir, "%(title)s.%(ext)s")
	args := []string{
		"-f", "bestaudio",
		"-x",
		"--audio-format", "mp3",
		"--audio-quality", "0",
		"--max-filesize", maxSourceFileSize,
		"--no-progress",
		"-o", outTemplate,
		"--",
		url,
	}

	if s.ffmpegLocation != "" {
		args = append([]string{"--ffmpeg-location", s.ffmpegLocation}, args...)
	}
	args = s.prependAuthFlags(args)

	runCtx, cancel := context.WithTimeout(ctx, s.downloadTimeout)
	defer cancel()
	_, stderr, err := execcmd.Run(runCtx, s.binary, args...)
	if err != nil {
		return "", s.classifiedFailure(fmt.Errorf("yt-dlp download: %w (stderr: %s)", err, stderr), stderr, ports.RunTimedOut(ctx, runCtx))
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

func (e ytDlpEntry) candidate() ports.AudioCandidate {
	return ports.AudioCandidate{
		Title:      e.Title,
		Duration:   e.Duration,
		URL:        e.WebpageURL,
		Channel:    e.Channel,
		Categories: e.Categories,
		ViewCount:  e.ViewCount,
	}
}
