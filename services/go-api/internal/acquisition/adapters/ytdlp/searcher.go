package ytdlp

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/execcmd"
	"altune/go-api/internal/shared/redact"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"

	sharedytdlp "altune/go-api/internal/shared/ytdlp"
)

const (
	searchTimeout   = 30 * time.Second
	downloadTimeout = 5 * time.Minute
	previewTimeout  = 60 * time.Second
)

const audioFormatSelector = "140/bestaudio[ext=m4a]/bestaudio/best[height<=480][protocol=https]/best[height<=480]/best"

const aacEncodeKbps = 128

const maxDownloadedFileBytes = 200 * 1024 * 1024

type searchRunner func(ctx context.Context, searchSpec string) ([]ports.AudioCandidate, error)

var searchEngines = []string{"ytsearch5:", "scsearch5:"}

const maxConcurrentSearches = 4

type YtDlpAudioSearcher struct {
	ffmpegLocation   string
	cookieFile       string
	jsRuntime        string
	remoteComponents string
	potURL           string
	binary           string
	runSearch        searchRunner
	inspect          inspectRunner
	inspections      *inspectionCache
	searchTimeout    time.Duration
	downloadTimeout  time.Duration
	previewTimeout   time.Duration
	canaryTimeout    time.Duration
}

const defaultRemoteComponents = "ejs:github"

func (s *YtDlpAudioSearcher) WithRemoteComponents(spec string) *YtDlpAudioSearcher {
	s.remoteComponents = spec
	return s
}

func (s *YtDlpAudioSearcher) WithPOTokenURL(url string) *YtDlpAudioSearcher {
	s.potURL = url
	return s
}

func NewYtDlpAudioSearcher(ffmpegLocation, cookieFile, jsRuntime string) *YtDlpAudioSearcher {
	s := &YtDlpAudioSearcher{
		ffmpegLocation:   ffmpegLocation,
		cookieFile:       cookieFile,
		jsRuntime:        jsRuntime,
		remoteComponents: defaultRemoteComponents,
		binary:           "yt-dlp",
		searchTimeout:    searchTimeout,
		downloadTimeout:  downloadTimeout,
		previewTimeout:   previewTimeout,
		canaryTimeout:    canaryTimeout,
		inspections:      newInspectionCache(),
	}
	s.runSearch = s.runYtDlpSearch
	s.inspect = s.runYtDlpInspect
	return s
}

const maxEmbeddedStderrBytes = 2048

const stderrTruncatedSuffix = "...(truncated)"

func embeddedStderr(stderr string) string {
	text := redact.LogText(stderr)
	if len(text) <= maxEmbeddedStderrBytes {
		return text
	}
	cut := maxEmbeddedStderrBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + stderrTruncatedSuffix
}

func (s *YtDlpAudioSearcher) Available() bool {
	return binpath.Runnable(s.binary)
}

func (s *YtDlpAudioSearcher) classifiedFailure(err error, stderr string, timedOut bool) error {
	if s.Available() && !timedOut && !sharedytdlp.OutputShowsSourceUnavailable(stderr) {
		return err
	}
	return &acqdomain.SourceUnavailableError{Source: SourceName, Err: err}
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
		prefix := []string{"--js-runtimes", s.jsRuntime}
		if s.remoteComponents != "" {
			prefix = append(prefix, "--remote-components", s.remoteComponents)
		}
		args = append(prefix, args...)
	}
	if s.potURL != "" {
		args = append([]string{"--extractor-args", "youtubepot-bgutilhttp:base_url=" + s.potURL}, args...)
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
		return nil, s.classifiedFailure(fmt.Errorf("yt-dlp search: %w (stderr: %s)", err, embeddedStderr(stderr)), stderr, ports.RunTimedOut(ctx, searchCtx))
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
		var entry Entry
		if err := json.Unmarshal(line, &entry); err != nil || entry.WebpageURL == "" {
			skipped++
			continue
		}
		candidates = append(candidates, CandidateFromEntry(entry))
	}
	return candidates, skipped
}

func (s *YtDlpAudioSearcher) Download(ctx context.Context, url, outDir string) (ports.FetchedAudio, error) {
	return s.fetchAudio(ctx, url, outDir, nil, s.downloadTimeout)
}

func (s *YtDlpAudioSearcher) DownloadPreview(ctx context.Context, url string, outDir string, seconds int) (string, error) {
	section := []string{"--download-sections", fmt.Sprintf("*0-%d", seconds)}
	fetched, err := s.fetchAudio(ctx, url, outDir, section, s.previewTimeout)
	return fetched.Path, err
}

const (
	sourceFormatPrefix = "altune-source-format:"
	outputPathPrefix   = "altune-output-path:"
)

func parseSourceFormat(stdout string) ports.SourceFormat {
	for line := range strings.Lines(stdout) {
		fields, ok := strings.CutPrefix(strings.TrimSpace(line), sourceFormatPrefix)
		if !ok {
			continue
		}
		parts := strings.Split(fields, "|")
		if len(parts) != 3 {
			return ports.SourceFormat{}
		}
		format := ports.SourceFormat{ID: parts[0], Codec: parts[1]}
		if kbps, err := strconv.ParseFloat(parts[2], 64); err == nil {
			format.Kbps = int(math.Round(kbps))
		}
		return format
	}
	return ports.SourceFormat{}
}

func (s *YtDlpAudioSearcher) fetchAudio(
	ctx context.Context,
	url, outDir string,
	sectionArgs []string,
	timeout time.Duration,
) (ports.FetchedAudio, error) {
	outTemplate := filepath.Join(outDir, "%(title)s.%(ext)s")
	args := []string{
		"-f", audioFormatSelector,
		"-x",
		"--audio-format", "m4a",
		"--audio-quality", fmt.Sprintf("%dK", aacEncodeKbps),
		"--max-filesize", strconv.Itoa(maxDownloadedFileBytes),
		"--no-progress",
		"--print", "after_move:" + sourceFormatPrefix + "%(format_id)s|%(acodec)s|%(abr)s",
		"--print", "after_move:" + outputPathPrefix + "%(filepath)s",
		"-o", outTemplate,
	}
	args = append(args, sectionArgs...)
	args = append(args, "--", ytDlpURL(url))

	if s.ffmpegLocation != "" {
		args = append([]string{"--ffmpeg-location", s.ffmpegLocation}, args...)
	}
	cookieFile, cleanup, err := s.cookieJarCopy("acquisition-download-cookies-*.txt")
	if err != nil {
		return ports.FetchedAudio{}, fmt.Errorf("yt-dlp download: copy cookie jar: %w", err)
	}
	defer cleanup()
	args = s.authFlags(args, cookieFile)

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout, stderr, err := execcmd.Run(runCtx, s.binary, args...)
	if err != nil {
		return ports.FetchedAudio{}, s.classifiedFailure(fmt.Errorf("yt-dlp download: %w (stderr: %s)", err, embeddedStderr(stderr)), stderr, ports.RunTimedOut(ctx, runCtx))
	}

	path, err := validatedOutputPath(outDir, parseOutputPath(stdout))
	if err != nil {
		return ports.FetchedAudio{}, err
	}
	return ports.FetchedAudio{Path: path, Format: parseSourceFormat(stdout)}, nil
}

func parseOutputPath(stdout string) string {
	for line := range strings.Lines(stdout) {
		if path, ok := strings.CutPrefix(strings.TrimSpace(line), outputPathPrefix); ok {
			return path
		}
	}
	return ""
}

func validatedOutputPath(outDir, printed string) (string, error) {
	if printed == "" {
		return "", errors.New("no output path printed by yt-dlp")
	}
	rel, err := filepath.Rel(outDir, printed)
	if err != nil || !filepath.IsLocal(rel) || filepath.Ext(printed) != ".m4a" {
		return "", fmt.Errorf("yt-dlp printed an output path outside %s or not an m4a: %s", outDir, printed)
	}
	info, err := os.Stat(printed)
	if err != nil {
		return "", fmt.Errorf("stat downloaded file in %s: %w", outDir, err)
	}
	const minFileSize = 10 * 1024
	if info.Size() < minFileSize {
		return "", fmt.Errorf("downloaded file too small (%d bytes), likely corrupt", info.Size())
	}
	if info.Size() > maxDownloadedFileBytes {
		return "", fmt.Errorf("downloaded file too large (%d bytes, cap %d), not a single track", info.Size(), maxDownloadedFileBytes)
	}
	return printed, nil
}

type Entry struct {
	Title      string   `json:"title"`
	Duration   float64  `json:"duration"`
	WebpageURL string   `json:"webpage_url"`
	Channel    string   `json:"channel"`
	Categories []string `json:"categories"`
	ViewCount  int64    `json:"view_count"`
}

func CandidateFromEntry(e Entry) ports.AudioCandidate {
	return ports.AudioCandidate{
		Title:      e.Title,
		Duration:   e.Duration,
		URL:        e.WebpageURL,
		Channel:    e.Channel,
		Categories: e.Categories,
		ViewCount:  e.ViewCount,
		Kind:       ports.KindUpload,
	}
}
