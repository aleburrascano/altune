package ytdlp

import (
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/childprocess"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/expiringcache"
	"altune/go-api/internal/shared/parallel"
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

	sharedytdlp "altune/go-api/internal/shared/ytdlprunner"
)

const (
	searchTimeout   = 30 * time.Second
	downloadTimeout = 5 * time.Minute
	previewTimeout  = 60 * time.Second
)

const audioFormatSelector = "140/bestaudio[ext=m4a]/bestaudio/best[height<=480][protocol=https]/best[height<=480]/best"

const aacEncodeKbps = 128

const maxDownloadedFileBytes = 200 * 1024 * 1024

var searchEngines = []string{"ytsearch5:", "scsearch5:"}

const maxConcurrentSearches = 4

const ytDlpBinary = "yt-dlp"

type YouTubeAudioSearcher struct {
	ffmpegLocation   string
	cookieFile       string
	environment      string
	jsRuntime        string
	remoteComponents string
	potURL           string
	inspections      *expiringcache.Memo[string, inspection]
	now              clock.Func
	sleep            clock.Sleeper
	searchTimeout    time.Duration
	downloadTimeout  time.Duration
	previewTimeout   time.Duration
	canaryTimeout    time.Duration
}

const defaultRemoteComponents = "ejs:github"

func (s *YouTubeAudioSearcher) WithRemoteComponents(spec string) *YouTubeAudioSearcher {
	s.remoteComponents = spec
	return s
}

func (s *YouTubeAudioSearcher) WithPOTokenURL(url string) *YouTubeAudioSearcher {
	s.potURL = url
	return s
}

func (s *YouTubeAudioSearcher) WithEnvironment(name string) *YouTubeAudioSearcher {
	s.environment = name
	return s
}

func (s *YouTubeAudioSearcher) WithDownloadTimeout(timeout time.Duration) *YouTubeAudioSearcher {
	if timeout > 0 {
		s.downloadTimeout = timeout
	}
	return s
}

func (s *YouTubeAudioSearcher) WithCanaryTimeout(timeout time.Duration) *YouTubeAudioSearcher {
	if timeout > 0 {
		s.canaryTimeout = timeout
	}
	return s
}

var (
	ErrSearcherClockRequired   = errors.New("the yt-dlp searcher needs a clock")
	ErrSearcherSleeperRequired = errors.New("the yt-dlp searcher needs a sleeper")
)

func NewYtDlpAudioSearcher(ffmpegLocation, cookieFile, jsRuntime string, now clock.Func, sleep clock.Sleeper) (*YouTubeAudioSearcher, error) {
	if err := errors.Join(
		requirePort(now == nil, "NewYtDlpAudioSearcher", ErrSearcherClockRequired),
		requirePort(sleep == nil, "NewYtDlpAudioSearcher", ErrSearcherSleeperRequired),
	); err != nil {
		return nil, err
	}
	s := &YouTubeAudioSearcher{
		ffmpegLocation:   ffmpegLocation,
		cookieFile:       cookieFile,
		jsRuntime:        jsRuntime,
		remoteComponents: defaultRemoteComponents,
		now:              now,
		sleep:            sleep,
		searchTimeout:    searchTimeout,
		downloadTimeout:  downloadTimeout,
		previewTimeout:   previewTimeout,
		canaryTimeout:    canaryTimeout,
	}
	s.inspections = expiringcache.New[string, inspection](inspectCacheTTL, inspectCacheMax, now)
	return s, nil
}

func MustNewYtDlpAudioSearcher(ffmpegLocation, cookieFile, jsRuntime string, now clock.Func, sleep clock.Sleeper) *YouTubeAudioSearcher {
	s, err := NewYtDlpAudioSearcher(ffmpegLocation, cookieFile, jsRuntime, now, sleep)
	if err != nil {
		panic(err)
	}
	return s
}

func requirePort(missing bool, owner string, err error) error {
	if !missing {
		return nil
	}
	return fmt.Errorf("%s: %w", owner, err)
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

func (s *YouTubeAudioSearcher) Available() bool {
	return binpath.Runnable(ytDlpBinary)
}

func (s *YouTubeAudioSearcher) classifiedFailure(err error, stderr string, timedOut bool) error {
	if s.Available() && !timedOut && !sharedytdlp.OutputShowsSourceUnavailable(stderr) {
		return err
	}
	return &acqdomain.SourceUnavailableError{Source: SourceName, Err: err}
}

var errSearchPanicked = errors.New("yt-dlp search panicked")

type pairOutcome struct {
	candidates []ports.AudioCandidate
	err        error
}

func (s *YouTubeAudioSearcher) runSearchPairs(ctx context.Context, queries []string) [][]pairOutcome {
	outcomes := make([][]pairOutcome, len(queries))
	for qi := range queries {
		outcomes[qi] = make([]pairOutcome, len(searchEngines))
	}

	g := parallel.New("acquisition.engine_search_panic", maxConcurrentSearches)
	for qi, query := range queries {
		for ei, engine := range searchEngines {
			spec := engine + query
			outcomes[qi][ei] = pairOutcome{err: errSearchPanicked}
			g.Go(ctx, func() {
				candidates, err := s.runYtDlpSearch(ctx, spec)
				outcomes[qi][ei] = pairOutcome{candidates: candidates, err: err}
			}, "spec", spec)
		}
	}
	_ = g.Wait()
	return outcomes
}

func (s *YouTubeAudioSearcher) foldEngineOutcomes(ctx context.Context, query string, outcomes []pairOutcome) ([]ports.AudioCandidate, error) {
	return ports.CollectCandidates(
		ctx,
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

func (s *YouTubeAudioSearcher) SearchQueries(ctx context.Context, queries []string) ([]ports.AudioCandidate, error) {
	outcomes := s.runSearchPairs(ctx, queries)

	return ports.CollectCandidates(
		ctx,
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

func (s *YouTubeAudioSearcher) jsRuntimeFlags() []string {
	if s.jsRuntime == "" {
		return nil
	}
	flags := []string{"--js-runtimes", s.jsRuntime}
	if s.remoteComponents != "" {
		flags = append(flags, "--remote-components", s.remoteComponents)
	}
	return flags
}

func (s *YouTubeAudioSearcher) authFlags(args []string, cookieFile string) []string {
	args = append(s.jsRuntimeFlags(), args...)
	if s.potURL != "" {
		args = append([]string{"--extractor-args", "youtubepot-bgutilhttp:base_url=" + s.potURL}, args...)
	}
	if cookieFile != "" {
		args = append([]string{"--cookies", cookieFile}, args...)
	}
	return args
}

func (s *YouTubeAudioSearcher) runYtDlpSearch(ctx context.Context, searchSpec string) ([]ports.AudioCandidate, error) {
	searchCtx, cancel := context.WithTimeout(ctx, s.searchTimeout)
	defer cancel()

	args := []string{
		"--dump-json",
		"--no-download",
		"--flat-playlist",
		"--",
		searchSpec,
	}
	cookies, err := s.leaseCookieJar(ctx, "search", searchSpec)
	if err != nil {
		return nil, fmt.Errorf("yt-dlp search: copy cookie jar: %w", err)
	}
	defer cookies.cleanup()
	args = s.authFlags(args, cookies.path)

	lines, stderr, err := sharedytdlp.DumpJSON(searchCtx, args)
	cookies.settle(ctx, err, stderr)
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

func (s *YouTubeAudioSearcher) Download(ctx context.Context, url, outDir string) (ports.FetchedAudio, error) {
	return s.fetchAudio(ctx, "download", url, outDir, nil, s.downloadTimeout)
}

func (s *YouTubeAudioSearcher) DownloadPreview(ctx context.Context, url, outDir string, seconds int) (string, error) {
	section := []string{"--download-sections", fmt.Sprintf("*0-%d", seconds)}
	fetched, err := s.fetchAudio(ctx, "preview", url, outDir, section, s.previewTimeout)
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

func (s *YouTubeAudioSearcher) fetchAudio(
	ctx context.Context,
	run, url, outDir string,
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
	cookies, err := s.leaseCookieJar(ctx, run, url)
	if err != nil {
		return ports.FetchedAudio{}, fmt.Errorf("yt-dlp download: copy cookie jar: %w", err)
	}
	defer cookies.cleanup()
	args = s.authFlags(args, cookies.path)

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout, stderr, err := childprocess.Run(runCtx, ytDlpBinary, args...)
	cookies.settle(ctx, err, stderr)
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
