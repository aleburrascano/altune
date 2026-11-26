package downloadsetup

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/youtubemusicapi"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const ytmSearchTimeout = 10 * time.Second

type SourceOrder struct {
	First    []download.AudioSource
	Fallback []download.AudioSource
}

func (t SourceOrder) All() []download.AudioSource {
	return append(append([]download.AudioSource{}, t.First...), t.Fallback...)
}

func EnabledSources(ctx context.Context, cfg *config.Config, redis *goredis.Client, transport http.RoundTripper) (SourceOrder, download.Verification, error) {
	searcher := YtDlpSearcher(cfg)
	tools := download.Verification{YtDlp: searcher.Available()}
	tiers, err := sourcesFor(ctx, cfg, redis, transport, searcher)
	return tiers, tools, err
}

func YtDlpSearcher(cfg *config.Config) *download.YouTubeAudioSearcher {
	return download.NewYtDlpAudioSearcher(cfg.FFmpegLocation, cfg.YtDLPCookieFile, cfg.YtDLPJSRuntime).
		WithRemoteComponents(cfg.YtDLPRemoteComponents).
		WithPOTokenURL(cfg.YtDLPPOTokenURL).
		WithEnvironment(cfg.Env)
}

func CanaryYTMClient(cfg *config.Config, redis *goredis.Client, transport http.RoundTripper) (*download.YouTubeMusicSearchClient, error) {
	return budgetedYTMSearchClient(context.Background(), cfg, redis, transport)
}

func budgetedYTMSearchClient(ctx context.Context, cfg *config.Config, redis *goredis.Client, transport http.RoundTripper) (*download.YouTubeMusicSearchClient, error) {
	return download.NewYtmSearch(download.YouTubeMusicSearchConfig{HTTP: innerTubeClient(ctx, cfg, redis, transport), Timeout: ytmSearchTimeout})
}

func innerTubeClient(ctx context.Context, cfg *config.Config, redis *goredis.Client, transport http.RoundTripper) *http.Client {
	settings := youtubemusicapi.Settings{MinInterval: cfg.YtmInnerTubeMinInterval, Cooldown: cfg.YtmInnerTubeCooldown}
	budget, err := youtubemusicapi.NewBudget(redis, settings)
	if err != nil {
		slog.WarnContext(ctx, "acquisition: no shared InnerTube budget without redis, pacing through the shared outbound pacer only")
		return &http.Client{Timeout: ytmSearchTimeout, Transport: transport}
	}
	return &http.Client{Timeout: ytmSearchTimeout, Transport: budget.Transport(transport)}
}

func sourcesFor(ctx context.Context, cfg *config.Config, redis *goredis.Client, transport http.RoundTripper, searcher *download.YouTubeAudioSearcher) (SourceOrder, error) {
	var tiers SourceOrder
	if cfg.YtMusicEnabled {
		songs, err := budgetedYTMSearchClient(ctx, cfg, redis, transport)
		if err != nil {
			return SourceOrder{}, fmt.Errorf("build the ytmusic search client: %w", err)
		}
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
		tiers.First = append([]download.AudioSource{download.NewSource(searcher, download.WithPageReader(download.NewHTTPPageReader(transport, cfg.AcquisitionFileHostPageTimeout)))}, tiers.First...)
	}
	return tiers, nil
}
