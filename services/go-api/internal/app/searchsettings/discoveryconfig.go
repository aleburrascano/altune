package searchsettings

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
)

const (
	discoveryProviderTimeout = 10 * time.Second
	discoveryChartTimeout    = 15 * time.Second
)

func Deps(ctx context.Context, cfg *config.Config, transport http.RoundTripper, metrics *telemetry.Registry) search.Deps {
	return search.Deps{
		Shutdown:        ctx.Done(),
		Logger:          slog.Default(),
		Transport:       transport,
		ProviderTimeout: discoveryProviderTimeout,
		ChartTimeout:    discoveryChartTimeout,
		CacheOps:        metrics.LabeledCounter(cacheOpsVarName),
		Providers:       discoveryProviderSettings(cfg),
		Search:          discoverySearchSettings(cfg),
		Unreleased:      search.UnreleasedSettings{ServeFromCache: cfg.UnreleasedSource == "cache", Tuning: unreleasedConfig(cfg)},
	}
}

func discoveryProviderSettings(cfg *config.Config) search.ProviderSettings {
	return search.ProviderSettings{
		MusicBrainzUserAgent: cfg.MusicBrainzUserAgent,
		DiscogsToken:         cfg.DiscogsToken,
		FanartTVAPIKey:       cfg.FanartTVAPIKey,
		GeniusAccessToken:    cfg.GeniusAccessToken,
		TheAudioDBAPIKey:     cfg.TheAudioDBAPIKey,
		GoogleSheetsAPIKey:   cfg.GoogleSheetsAPIKey,
		TrackerHubSheetID:    cfg.TrackerHubSheetID,
	}
}

func discoverySearchSettings(cfg *config.Config) search.SearchSettings {
	return search.SearchSettings{
		Deadline:            cfg.DiscoverySearchDeadline,
		TailDemotion:        cfg.TailDemotionEnabled,
		CrossKindProminence: cfg.CrossKindProminenceEnabled,
		Exploration:         cfg.ExplorationEnabled,
		ExplorationRate:     cfg.ExplorationRate,
		BehavioralRanking:   cfg.BehavioralRankingEnabled,
		ProviderCacheTTL:    cfg.DiscoveryProviderCacheTTL,
	}
}

func unreleasedConfig(cfg *config.Config) search.UnreleasedConfig {
	return search.UnreleasedConfig{
		MaxPages:          cfg.GeniusUnreleasedMaxPages,
		MaxDetails:        cfg.GeniusUnreleasedMaxDetails,
		DetailConcurrency: cfg.GeniusUnreleasedDetailConcurrency,
		TTL:               cfg.GeniusUnreleasedTTL,
		EmptyTTL:          cfg.GeniusUnreleasedEmptyTTL,
		Wait:              cfg.GeniusUnreleasedWait,
		JobTimeout:        cfg.GeniusUnreleasedJobTimeout,
	}
}

func ClientEvents(cfg *config.Config) search.ClientEventSink {
	if !cfg.HasTelemetryExport() {
		return search.NoClientEvents{}
	}
	return telemetry.NewClientEvents(clock.Now, cfg.TelemetryUserKey, otel.GetLoggerProvider().Logger(telemetry.ScopeName))
}

const cacheOpsVarName = "discovery_cache_ops"
