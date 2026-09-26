package commands

import (
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	"altune/go-api/internal/app"
	"altune/go-api/internal/discovery/domain"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	journeySearchQuery   = "Bohemian Rhapsody"
	journeySearchLimit   = 10
	journeySearchTimeout = 60 * time.Second
)

var (
	errJourneyNoResults = errors.New("no results")
	errJourneyEmptyFile = errors.New("downloaded file is empty")
)

type journeySearcher interface {
	Execute(ctx context.Context, userId shared.UserId, query *domain.SearchQuery, saveHistory bool) (*discoveryService.SearchOutput, error)
}

type journeyDownloader interface {
	Download(ctx context.Context, url string, outDir string) (string, error)
}

func RunJourneyCheck(cfg *config.Config) {
	exitOnError(runJourneyCheckWithConfig(cfg))
}

func runJourneyCheckWithConfig(cfg *config.Config) error {
	ctx := context.Background()
	pool, err := openPool(ctx, cfg)
	if err != nil {
		return journeyStepFailed("search", err)
	}
	defer pool.Close()

	cookieJar, removeCookieJar, err := throwawayCookieJar(cfg.YtDLPCookieFile)
	if err != nil {
		return journeyStepFailed("download", err)
	}
	defer removeCookieJar()

	searcher := app.BuildRankingOnlySearchService(cfg, pool, nil, nil)
	downloader := ytdlp.NewYtDlpAudioSearcher(cfg.FFmpegLocation, cookieJar, cfg.YtDLPJSRuntime)
	return runJourneyCheck(ctx, searcher, downloader, os.Stdout)
}

func throwawayCookieJar(livePath string) (path string, remove func(), err error) {
	if livePath == "" {
		return "", func() {}, nil
	}
	live, err := os.Open(livePath)
	if err != nil {
		return "", func() {}, err
	}
	defer func() { _ = live.Close() }()

	jar, err := os.CreateTemp("", "journey-check-cookies-*.txt")
	if err != nil {
		return "", func() {}, err
	}
	remove = func() { _ = os.Remove(jar.Name()) }
	_, copyErr := io.Copy(jar, live)
	if err := errors.Join(copyErr, jar.Close()); err != nil {
		remove()
		return "", func() {}, err
	}
	return jar.Name(), remove, nil
}

func runJourneyCheck(ctx context.Context, searcher journeySearcher, downloader journeyDownloader, out io.Writer) error {
	resultCount, err := journeySearch(ctx, searcher)
	if err != nil {
		return journeyStepFailed("search", err)
	}
	_, _ = fmt.Fprintf(out, "journey-check: search ok (%d results)\n", resultCount)

	size, err := journeyDownload(ctx, downloader)
	if err != nil {
		return journeyStepFailed("download", err)
	}
	_, _ = fmt.Fprintf(out, "journey-check: download ok (%d bytes)\n", size)
	return nil
}

func journeySearch(ctx context.Context, searcher journeySearcher) (int, error) {
	kinds := map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
	query, err := domain.NewSearchQuery(journeySearchQuery, kinds, journeySearchLimit)
	if err != nil {
		return 0, err
	}
	searchCtx, cancel := context.WithTimeout(ctx, journeySearchTimeout)
	defer cancel()
	output, err := searcher.Execute(searchCtx, shared.SystemUserId(), query, false)
	if err != nil {
		return 0, err
	}
	if len(output.Results) == 0 {
		return 0, errJourneyNoResults
	}
	return len(output.Results), nil
}

func journeyDownload(ctx context.Context, downloader journeyDownloader) (int64, error) {
	outDir, err := os.MkdirTemp("", "journey-check-*")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.RemoveAll(outDir) }()

	path, err := downloader.Download(ctx, ytdlp.YouTubeCanary.URL, outDir)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if info.Size() == 0 {
		return 0, errJourneyEmptyFile
	}
	return info.Size(), nil
}

func journeyStepFailed(step string, err error) error {
	return fmt.Errorf("journey-check: %s failed: %s", step, redact.LogText(err.Error()))
}
