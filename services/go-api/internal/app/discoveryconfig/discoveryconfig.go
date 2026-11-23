package discoveryconfig

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"expvar"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
)

func Deps(ctx context.Context, cfg *config.Config, transport http.RoundTripper) discovery.Deps {
	return discovery.Deps{
		Lifecycle:       ctx,
		Logger:          slog.Default(),
		Transport:       transport,
		ProviderTimeout: 10 * time.Second,
		ChartTimeout:    15 * time.Second,
		CacheOps:        cacheOps(),
		Providers:       discoveryProviderSettings(cfg),
		Search:          discoverySearchSettings(cfg),
		Unreleased:      discovery.UnreleasedSettings{ServeFromCache: cfg.UnreleasedSource == "cache", Tuning: unreleasedConfig(cfg)},
	}
}

func discoveryProviderSettings(cfg *config.Config) discovery.ProviderSettings {
	return discovery.ProviderSettings{
		MusicBrainzUserAgent: cfg.MusicBrainzUserAgent,
		DiscogsToken:         cfg.DiscogsToken,
		FanartTVAPIKey:       cfg.FanartTVAPIKey,
		GeniusAccessToken:    cfg.GeniusAccessToken,
		TheAudioDBAPIKey:     cfg.TheAudioDBAPIKey,
		GoogleSheetsAPIKey:   cfg.GoogleSheetsAPIKey,
		TrackerHubSheetID:    cfg.TrackerHubSheetID,
	}
}

func discoverySearchSettings(cfg *config.Config) discovery.SearchSettings {
	return discovery.SearchSettings{
		Deadline:            cfg.DiscoverySearchDeadline,
		TailDemotion:        cfg.TailDemotionEnabled,
		CrossKindProminence: cfg.CrossKindProminenceEnabled,
		Exploration:         cfg.ExplorationEnabled,
		ExplorationRate:     cfg.ExplorationRate,
		BehavioralRanking:   cfg.BehavioralRankingEnabled,
		ProviderCacheTTL:    cfg.DiscoveryProviderCacheTTL,
	}
}

func unreleasedConfig(cfg *config.Config) discovery.UnreleasedConfig {
	return discovery.UnreleasedConfig{
		MaxPages:          cfg.GeniusUnreleasedMaxPages,
		MaxDetails:        cfg.GeniusUnreleasedMaxDetails,
		DetailConcurrency: cfg.GeniusUnreleasedDetailConcurrency,
		TTL:               cfg.GeniusUnreleasedTTL,
		EmptyTTL:          cfg.GeniusUnreleasedEmptyTTL,
		Wait:              cfg.GeniusUnreleasedWait,
		JobTimeout:        cfg.GeniusUnreleasedJobTimeout,
	}
}

func ClientEvents(cfg *config.Config) *telemetry.ClientEvents {
	if !cfg.HasTelemetryExport() {
		return nil
	}
	return telemetry.NewClientEvents(cfg.TelemetryUserKey, otel.GetLoggerProvider().Logger(telemetry.ScopeName))
}

const cacheOpsVarName = "discovery_cache_ops"

func cacheOps() *expvar.Map {
	ops, ok := expvar.Get(cacheOpsVarName).(*expvar.Map)
	if !ok {
		ops = expvar.NewMap(cacheOpsVarName)
	}
	return ops
}
