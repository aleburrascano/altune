package audioacquisition

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/app/providerhttp"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/ytminnertube"
	"context"
	"log/slog"
	"net/http"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	fileHostPageTimeout  = 10 * time.Second
	ytmSearchMinInterval = 200 * time.Millisecond
	ytmSearchTimeout     = 10 * time.Second
)

type Tiers struct {
	First    []acquisition.AudioSource
	Fallback []acquisition.AudioSource
}

func (t Tiers) All() []acquisition.AudioSource {
	return append(append([]acquisition.AudioSource{}, t.First...), t.Fallback...)
}

func LiveTiers(ctx context.Context, cfg *config.Config, redis *goredis.Client) (Tiers, acquisition.AcquisitionVerification) {
	searcher := YtDlpSearcher(cfg)
	tools := acquisition.AcquisitionVerification{YtDlp: searcher.Available()}
	return sourcesFor(ctx, cfg, redis, searcher), tools
}

func YtDlpSearcher(cfg *config.Config) *acquisition.YtDlpAudioSearcher {
	return acquisition.NewYtDlpAudioSearcher(cfg.FFmpegLocation, cfg.YtDLPCookieFile, cfg.YtDLPJSRuntime).
		WithRemoteComponents(cfg.YtDLPRemoteComponents).
		WithPOTokenURL(cfg.YtDLPPOTokenURL).
		WithEnvironment(cfg.Env)
}

func CanaryYTMClient(cfg *config.Config, redis *goredis.Client) *acquisition.YtmSearchClient {
	return budgetedYTMSearchClient(context.Background(), cfg, redis)
}

func budgetedYTMSearchClient(ctx context.Context, cfg *config.Config, redis *goredis.Client) *acquisition.YtmSearchClient {
	return acquisition.NewYtmSearch(acquisition.YtmSearchConfig{HTTP: innerTubeClient(ctx, cfg, redis), MinInterval: ytmSearchMinInterval, Timeout: ytmSearchTimeout})
}

func innerTubeClient(ctx context.Context, cfg *config.Config, redis *goredis.Client) *http.Client {
	settings := ytminnertube.Settings{MinInterval: cfg.YtmInnerTubeMinInterval, Cooldown: cfg.YtmInnerTubeCooldown}
	budget, err := ytminnertube.NewBudget(redis, settings)
	if err != nil {
		slog.WarnContext(ctx, "acquisition: no shared InnerTube budget without redis, pacing in-process only")
		return nil
	}
	return &http.Client{Transport: budget.Transport(nil)}
}

func sourcesFor(ctx context.Context, cfg *config.Config, redis *goredis.Client, searcher *acquisition.YtDlpAudioSearcher) Tiers {
	var tiers Tiers
	if cfg.YtMusicEnabled {
		songs := budgetedYTMSearchClient(ctx, cfg, redis)
		tiers.First = append(tiers.First, acquisition.NewYtmSearchSource(songs, searcher), acquisition.NewYtMusicSource(searcher, songs))
	} else {
		slog.InfoContext(ctx, "acquisition: ytmusic source disabled via YTMUSIC_ENABLED")
	}
	if cfg.YtDLPEnabled {
		tiers.Fallback = append(tiers.Fallback, acquisition.NewYtDlpSource(searcher))
	} else {
		slog.InfoContext(ctx, "acquisition: yt-dlp source disabled via YTDLP_ENABLED")
	}
	if len(tiers.All()) > 0 {
		tiers.First = append([]acquisition.AudioSource{acquisition.NewSource(searcher, acquisition.WithPageReader(acquisition.NewHTTPPageReader(providerhttp.Live(), fileHostPageTimeout)))}, tiers.First...)
	}
	return tiers
}
