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

	"golang.org/x/sync/errgroup"

	sharedytdlp "altune/go-api/internal/shared/ytdlp"
)

const (
	searchTimeout   = 30 * time.Second
	downloadTimeout = 5 * time.Minute
	previewTimeout  = 60 * time.Second
)

const maxSourceFileSize = "200M"

const audioFormatSelector = "bestaudio/best[height<=480][protocol=https]/best[height<=480]/best"

const maxDownloadedFileBytes = 200 * 1024 * 1024

type searchRunner func(ctx context.Context, searchSpec string) ([]ports.AudioCandidate, error)

var searchEngines = []string{"ytsearch5:", "scsearch5:"}

const maxConcurrentSearches = 4

type YtDlpAudioSearcher struct {
	ffmpegLocation  string
	cookieFile      string
	jsRuntime       string
	binary          string
	runSearch       searchRunner
	inspect         inspectRunner
	inspections     *inspectionCache
	searchTimeout   time.Duration
	downloadTimeout time.Duration
	previewTimeout  time.Duration
	canaryTimeout   time.Duration
}

func NewYtDlpAudioSearcher(ffmpegLocation, cookieFile, jsRuntime string) *YtDlpAudioSearcher {
	s := &YtDlpAudioSearcher{
		ffmpegLocation:  ffmpegLocation,
		cookieFile:      cookieFile,
		jsRuntime:       jsRuntime,
		binary:          "yt-dlp",
		searchTimeout:   searchTimeout,
		downloadTimeout: downloadTimeout,
		previewTimeout:  previewTimeout,
		canaryTimeout:   canaryTimeout,
		inspections:     newInspectionCache(),
	}
	s.runSearch = s.runYtDlpSearch
	s.inspect = s.runYtDlpInspect
	return s
}

func (s *YtDlpAudioSearcher) Available() bool {
	return binpath.Runnable(s.binary)
}

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

type pairOutcome struct {
	candidates []ports.AudioCandidate
	err        error
}

func (s *YtDlpAudioSearcher) runSearchPairs(ctx context.Context, queries []string) [][]pairOutcome {
	outcomes := make([][]pairOutcome, len(queries))
	for qi := range queries {
		outcomes[qi] = make([]pairOutcome, len(searchEngines))
	}

	g := new(errgroup.Group)
	g.SetLimit(maxConcurrentSearches)
	for qi, query := range queries {
		for ei, engine := range searchEngines {
			spec := engine + query
			g.Go(func() error {
				candidates, err := s.runSearch(ctx, spec)
				outcomes[qi][ei] = pairOutcome{candidates: candidates, err: err}
				return nil
			})
		}
	}
	_ = g.Wait()
	return outcomes
}

func (s *YtDlpAudioSearcher) foldEngineOutcomes(ctx context.Context, query string, outcomes []pairOutcome) ([]ports.AudioCandidate, error) {
	return ports.CollectCandidates(
		len(outcomes),
		func(ei int) ([]ports.AudioCandidate, error) {
			return outcomes[ei].candidates, outcomes[ei].err
		},
		func(ei int, candidates []ports.AudioCandidate) {
			slog.InfoContext(ctx, "acquisition.engine_search_results",
				"spec", searchEngines[ei]+query, "candidates", len(candidates))
		},
		func(ei int, err error) {
			slog.WarnContext(ctx, "acquisition.engine_search_failed",
				"spec", searchEngines[ei]+query, "error", redact.LogError(err))
		},
		func(firstErr error) error {
			return fmt.Errorf("all search engines failed: %w", firstErr)
		},
	)
}

func (s *YtDlpAudioSearcher) SearchQueries(ctx context.Context, queries []string) ([]ports.AudioCandidate, error) {
	outcomes := s.runSearchPairs(ctx, queries)

	return ports.CollectCandidates(
		len(queries),
		func(qi int) ([]ports.AudioCandidate, error) {
			return s.foldEngineOutcomes(ctx, queries[qi], outcomes[qi])
		},
		func(qi int, candidates []ports.AudioCandidate) {
			slog.InfoContext(ctx, "acquisition.search_query_results",
				"query", queries[qi], "candidates", len(candidates))
		},
		func(qi int, err error) {
			slog.WarnContext(ctx, "acquisition.search_query_failed",
				"query", queries[qi], "error", redact.LogError(err))
		},
		func(firstErr error) error { return firstErr },
	)
}

func (s *YtDlpAudioSearcher) authFlags(args []string, cookieFile string) []string {
	if s.jsRuntime != "" {
		args = append([]string{"--js-runtimes", s.jsRuntime, "--remote-components", "ejs:github"}, args...)
	}
	if cookieFile != "" {
		args = append([]string{"--cookies", cookieFile}, args...)
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
	cookieFile, cleanup, err := s.cookieJarCopy("acquisition-search-cookies-*.txt")
	if err != nil {
		return nil, fmt.Errorf("yt-dlp search: copy cookie jar: %w", err)
	}
	defer cleanup()
	args = s.authFlags(args, cookieFile)

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
	return s.fetchAudio(ctx, url, outDir, nil, s.downloadTimeout)
}

func (s *YtDlpAudioSearcher) DownloadPreview(ctx context.Context, url string, outDir string, seconds int) (string, error) {
	section := []string{"--download-sections", fmt.Sprintf("*0-%d", seconds)}
	return s.fetchAudio(ctx, url, outDir, section, s.previewTimeout)
}

func (s *YtDlpAudioSearcher) fetchAudio(
	ctx context.Context,
	url, outDir string,
	sectionArgs []string,
	timeout time.Duration,
) (string, error) {
	outTemplate := filepath.Join(outDir, "%(title)s.%(ext)s")
	args := []string{
		"-f", audioFormatSelector,
		"-x",
		"--audio-format", "mp3",
		"--audio-quality", "0",
		"--max-filesize", maxSourceFileSize,
		"--no-progress",
		"-o", outTemplate,
	}
	args = append(args, sectionArgs...)
	args = append(args, "--", url)

	if s.ffmpegLocation != "" {
		args = append([]string{"--ffmpeg-location", s.ffmpegLocation}, args...)
	}
	cookieFile, cleanup, err := s.cookieJarCopy("acquisition-download-cookies-*.txt")
	if err != nil {
		return "", fmt.Errorf("yt-dlp download: copy cookie jar: %w", err)
	}
	defer cleanup()
	args = s.authFlags(args, cookieFile)

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, stderr, err := execcmd.Run(runCtx, s.binary, args...)
	if err != nil {
		return "", s.classifiedFailure(fmt.Errorf("yt-dlp download: %w (stderr: %s)", err, stderr), stderr, ports.RunTimedOut(ctx, runCtx))
	}

	return largestMP3(outDir)
}

func largestMP3(outDir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(outDir, "*.mp3"))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no mp3 file produced in %s", outDir)
	}

	best, bestSize := largestReadable(matches)
	if best == "" {
		return "", fmt.Errorf("stat downloaded files in %s", outDir)
	}
	const minFileSize = 10 * 1024
	if bestSize < minFileSize {
		return "", fmt.Errorf("downloaded file too small (%d bytes), likely corrupt", bestSize)
	}
	if bestSize > maxDownloadedFileBytes {
		return "", fmt.Errorf("downloaded file too large (%d bytes, cap %d), not a single track", bestSize, maxDownloadedFileBytes)
	}

	return best, nil
}

func largestReadable(paths []string) (string, int64) {
	best, bestSize := "", int64(-1)
	for _, m := range paths {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		if info.Size() > bestSize {
			best, bestSize = m, info.Size()
		}
	}
	return best, bestSize
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
