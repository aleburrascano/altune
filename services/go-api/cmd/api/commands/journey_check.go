package commands

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/app"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
	shared "altune/go-api/internal/shared/kernel"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
	Execute(ctx context.Context, userId shared.UserId, query *discovery.SearchQuery, saveHistory bool) (*discovery.SearchOutput, error)
}

type journeyDownloader interface {
	Download(ctx context.Context, url string, outDir string) (acquisition.FetchedAudio, error)
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

	searcher, err := rankingOnlySearcher(ctx, cfg, pool)
	if err != nil {
		return journeyStepFailed("search", err)
	}
	downloader := acquisition.NewYtDlpAudioSearcher(cfg.FFmpegLocation, cookieJar, cfg.YtDLPJSRuntime).WithPOTokenURL(cfg.YtDLPPOTokenURL)
	return runJourneyCheck(ctx, searcher, downloader, os.Stdout)
}

func rankingOnlySearcher(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool) (journeySearcher, error) {
	disc, err := app.NewEvalDiscovery(ctx, cfg, app.EvalHandles{Pool: pool})
	if err != nil {
		return nil, err
	}
	return disc.RankingOnlySearch(), nil
}

func throwawayCookieJar(livePath string) (path string, remove func(), err error) {
	if livePath == "" {
		return "", func() {}, nil
	}
	return acquisition.CopyToTempFile(livePath, "journey-check-cookies-*.txt")
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
	kinds := map[discovery.ResultKind]bool{
		discovery.ResultKindTrack:  true,
		discovery.ResultKindAlbum:  true,
		discovery.ResultKindArtist: true,
	}
	query, err := discovery.NewSearchQuery(journeySearchQuery, kinds, journeySearchLimit)
	if err != nil {
		return 0, err
	}
	searchCtx, cancel := context.WithTimeout(ctx, journeySearchTimeout)
	defer cancel()
	output, err := searcher.Execute(searchCtx, shared.SystemUserId(), query, false)
	if err != nil {
		return 0, err
	}
	if output == nil || len(output.Results) == 0 {
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

	fetched, err := downloader.Download(ctx, acquisition.YouTubeCanary.URL, outDir)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(fetched.Path)
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
