package download

import (
	"altune/go-api/internal/download/internal/adapters/savedlink"
	"altune/go-api/internal/download/internal/adapters/youtubemusic"
	"altune/go-api/internal/download/internal/adapters/ytdlp"
	"altune/go-api/internal/download/internal/adapters/ytmsearch"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/youtubemusicapi"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const (
	ytmSearchTimeout = 10 * time.Second
	acoustIDTimeout  = 15 * time.Second
)

func EnabledSources(ctx context.Context, d SourceDeps) (SourceOrder, ports.Verification, error) {
	searcher := YtDlpSearcher(d.Config)
	tools := ports.Verification{YtDlp: searcher.Available()}
	tiers, err := sourcesFor(ctx, d, searcher)
	return tiers, tools, err
}

func YtDlpSearcher(cfg *config.Config) *ytdlp.YouTubeAudioSearcher {
	return ytdlp.MustNewYtDlpAudioSearcher(cfg.FFmpegLocation, cfg.YtDLPCookieFile, cfg.YtDLPJSRuntime, clock.Now, clock.Sleep,
		ytdlp.WithRemoteComponents(cfg.YtDLPRemoteComponents),
		ytdlp.WithPOTokenURL(cfg.YtDLPPOTokenURL),
		ytdlp.WithEnvironment(cfg.Env))
}

func CanaryYTMClient(ctx context.Context, d SourceDeps) (*ytmsearch.Client, error) {
	return budgetedYTMSearchClient(ctx, d)
}

func budgetedYTMSearchClient(ctx context.Context, d SourceDeps) (*ytmsearch.Client, error) {
	client, err := ytmsearch.ForClient(innerTubeClient(ctx, d))
	if err != nil {
		return nil, fmt.Errorf("for client: %w", err)
	}
	return client, nil
}

func innerTubeClient(ctx context.Context, d SourceDeps) *http.Client {
	cfg := d.Config
	settings := youtubemusicapi.Settings{MinInterval: cfg.YtmInnerTubeMinInterval, Cooldown: cfg.YtmInnerTubeCooldown}
	budget, err := youtubemusicapi.NewBudget(d.Redis, settings, clock.Now, clock.Sleep)
	if err != nil {
		slog.WarnContext(ctx, "acquisition: no shared InnerTube budget without redis, pacing through the shared outbound pacer only")
		return d.NewClient(ytmSearchTimeout, d.Transport)
	}
	return d.NewClient(ytmSearchTimeout, budget.Transport(d.Transport))
}

func sourcesFor(ctx context.Context, d SourceDeps, searcher *ytdlp.YouTubeAudioSearcher) (SourceOrder, error) {
	cfg := d.Config
	var tiers SourceOrder
	switch {
	case cfg.YtMusicEnabled:
		songs, err := budgetedYTMSearchClient(ctx, d)
		if err != nil {
			return SourceOrder{}, fmt.Errorf("build the ytmusic search client: %w", err)
		}
		tiers.First = append(tiers.First, ytmsearch.NewSource(songs, searcher), youtubemusic.NewSource(searcher, songs))
	default:
		slog.InfoContext(ctx, "acquisition: ytmusic source disabled via YTMUSIC_ENABLED")
	}
	switch {
	case cfg.YtDLPEnabled:
		tiers.Fallback = append(tiers.Fallback, ytdlp.NewSource(searcher))
	default:
		slog.InfoContext(ctx, "acquisition: yt-dlp source disabled via YTDLP_ENABLED")
	}
	if len(tiers.All()) > 0 {
		tiers.First = append([]ports.AudioSource{savedlink.MustNewSource(searcher, savedlink.NewHTTPPageReader(d.Transport, cfg.AcquisitionFileHostPageTimeout))}, tiers.First...)
	}
	return tiers, nil
}
