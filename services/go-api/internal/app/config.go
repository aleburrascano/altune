package app

import (
	"altune/go-api/internal/platform/monitoring"
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
	searchProviderTimeout = 10 * time.Second
	searchChartTimeout    = 15 * time.Second
	searchCacheOpsName    = "discovery_cache_ops"
)

func searchBaseDeps(ctx context.Context, cfg *config.Config, transport http.RoundTripper, metrics *telemetry.Registry) search.Deps {
	return search.Deps{
		Shutdown:        ctx.Done(),
		Logger:          slog.Default(),
		Transport:       transport,
		ProviderTimeout: searchProviderTimeout,
		ChartTimeout:    searchChartTimeout,
		CacheOps:        metrics.LabeledCounter(searchCacheOpsName),
		Providers:       searchProviderSettings(cfg),
		Search:          searchSettings(cfg),
		RateLimits:      search.DefaultDiscoveryRateLimits,
		Unreleased:      search.UnreleasedSettings{ServeFromCache: cfg.UnreleasedSource == "cache", Tuning: searchUnreleasedConfig(cfg)},
	}
}

func searchProviderSettings(cfg *config.Config) search.ProviderSettings {
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

func searchSettings(cfg *config.Config) search.SearchSettings {
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

func searchUnreleasedConfig(cfg *config.Config) search.UnreleasedConfig {
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

func clientEventSink(cfg *config.Config) search.ClientEventSink {
	if !cfg.HasTelemetryExport() {
		return search.NoClientEvents{}
	}
	return telemetry.NewClientEvents(clock.Now, cfg.TelemetryUserKey, otel.GetLoggerProvider().Logger(telemetry.ScopeName))
}

func alertNotifierSettings(cfg *config.Config) monitoring.NotifierSettings {
	return monitoring.NotifierSettings{
		ResendAPIKey: cfg.ResendAPIKey,
		EmailTo:      cfg.AlertEmailTo,
		WebhookURL:   cfg.AlertWebhookURL,
	}
}
