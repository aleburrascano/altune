package journey

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/app/providertransport"
	"altune/go-api/internal/download"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/redact"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	searchQuery   = "Bohemian Rhapsody"
	searchLimit   = 10
	searchTimeout = 60 * time.Second
)

var (
	errNoResults = errors.New("no results")
	errEmptyFile = errors.New("downloaded file is empty")
)

type Searcher interface {
	Execute(ctx context.Context, userId ids.UserId, query *search.SearchQuery) (*search.SearchOutput, error)
}

type Downloader interface {
	Download(ctx context.Context, url string, outDir string) (download.FetchedAudio, error)
}

func RankingOnlySearcher(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool) (Searcher, error) {
	tracker, err := app.NewTracker(cfg)
	if err != nil {
		return nil, err
	}
	metrics := telemetry.NewRegistry()
	transport, err := providertransport.New(metrics)
	if err != nil {
		return nil, err
	}
	disc, err := app.NewEvalDiscovery(ctx, cfg, app.EvalHandles{Pool: pool, Transport: transport, Tracker: tracker, Metrics: metrics})
	if err != nil {
		return nil, err
	}
	return disc.RankingOnlySearch(), nil
}

func Run(ctx context.Context, searcher Searcher, downloader Downloader, out io.Writer) error {
	resultCount, err := runSearch(ctx, searcher)
	if err != nil {
		return StepFailed("search", err)
	}
	_, _ = fmt.Fprintf(out, "journey-check: search ok (%d results)\n", resultCount)

	size, err := runDownload(ctx, downloader)
	if err != nil {
		return StepFailed("download", err)
	}
	_, _ = fmt.Fprintf(out, "journey-check: download ok (%d bytes)\n", size)
	return nil
}

func runSearch(ctx context.Context, searcher Searcher) (int, error) {
	kinds := map[search.ResultKind]bool{
		search.ResultKindTrack:  true,
		search.ResultKindAlbum:  true,
		search.ResultKindArtist: true,
	}
	query, err := search.NewSearchQuery(searchQuery, kinds, searchLimit)
	if err != nil {
		return 0, err
	}
	searchCtx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	output, err := searcher.Execute(searchCtx, ids.SystemUserId(), query)
	if err != nil {
		return 0, err
	}
	if len(output.Results) == 0 {
		return 0, errNoResults
	}
	return len(output.Results), nil
}

func runDownload(ctx context.Context, downloader Downloader) (int64, error) {
	outDir, err := os.MkdirTemp("", "journey-check-*")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.RemoveAll(outDir) }()

	fetched, err := downloader.Download(ctx, download.YouTubeCanary.URL, outDir)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(fetched.Path)
	if err != nil {
		return 0, err
	}
	if info.Size() == 0 {
		return 0, errEmptyFile
	}
	return info.Size(), nil
}

func StepFailed(step string, err error) error {
	return fmt.Errorf("journey-check: %s failed: %s", step, redact.LogText(err.Error()))
}
