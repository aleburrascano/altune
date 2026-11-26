package downloadsetup

import (
	"altune/go-api/internal/app/providertransport"
	"altune/go-api/internal/download"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/youtubemusicapi"
	"context"
	"log/slog"
	"net/http"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	ytmSearchMinInterval = 200 * time.Millisecond
	ytmSearchTimeout     = 10 * time.Second
)

type SourceOrder struct {
	First    []download.AudioSource
	Fallback []download.AudioSource
}

func (t SourceOrder) All() []download.AudioSource {
	return append(append([]download.AudioSource{}, t.First...), t.Fallback...)
}

func EnabledSources(ctx context.Context, cfg *config.Config, redis *goredis.Client) (SourceOrder, download.Verification) {
	searcher := YtDlpSearcher(cfg)
	tools := download.Verification{YtDlp: searcher.Available()}
	return sourcesFor(ctx, cfg, redis, searcher), tools
}

func YtDlpSearcher(cfg *config.Config) *download.YouTubeAudioSearcher {
	return download.NewYtDlpAudioSearcher(cfg.FFmpegLocation, cfg.YtDLPCookieFile, cfg.YtDLPJSRuntime).
		WithRemoteComponents(cfg.YtDLPRemoteComponents).
		WithPOTokenURL(cfg.YtDLPPOTokenURL).
		WithEnvironment(cfg.Env)
}

func CanaryYTMClient(cfg *config.Config, redis *goredis.Client) *download.YouTubeMusicSearchClient {
	return budgetedYTMSearchClient(context.Background(), cfg, redis)
}

func budgetedYTMSearchClient(ctx context.Context, cfg *config.Config, redis *goredis.Client) *download.YouTubeMusicSearchClient {
	return download.NewYtmSearch(download.YouTubeMusicSearchConfig{HTTP: innerTubeClient(ctx, cfg, redis), MinInterval: ytmSearchMinInterval, Timeout: ytmSearchTimeout})
}

func innerTubeClient(ctx context.Context, cfg *config.Config, redis *goredis.Client) *http.Client {
	settings := youtubemusicapi.Settings{MinInterval: cfg.YtmInnerTubeMinInterval, Cooldown: cfg.YtmInnerTubeCooldown}
	budget, err := youtubemusicapi.NewBudget(redis, settings)
	if err != nil {
		slog.WarnContext(ctx, "acquisition: no shared InnerTube budget without redis, pacing in-process only")
		return nil
	}
	return &http.Client{Transport: budget.Transport(nil)}
}

func sourcesFor(ctx context.Context, cfg *config.Config, redis *goredis.Client, searcher *download.YouTubeAudioSearcher) SourceOrder {
	var tiers SourceOrder
	if cfg.YtMusicEnabled {
		songs := budgetedYTMSearchClient(ctx, cfg, redis)
		tiers.First = append(tiers.First, download.NewYtmSearchSource(songs, searcher), download.NewYtMusicSource(searcher, songs))
	} else {
		slog.InfoContext(ctx, "acquisition: ytmusic source disabled via YTMUSIC_ENABLED")
	}
	if cfg.YtDLPEnabled {
		tiers.Fallback = append(tiers.Fallback, download.NewYtDlpSource(searcher))
	} else {
		slog.InfoContext(ctx, "acquisition: yt-dlp source disabled via YTDLP_ENABLED")
	}
	if len(tiers.All()) > 0 {
		tiers.First = append([]download.AudioSource{download.NewSource(searcher, download.WithPageReader(download.NewHTTPPageReader(providertransport.Live(), cfg.AcquisitionFileHostPageTimeout)))}, tiers.First...)
	}
	return tiers
}
