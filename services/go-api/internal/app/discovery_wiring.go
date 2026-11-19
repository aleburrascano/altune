package app

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"expvar"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel"
)

func (a *App) discoveryDeps(ctx context.Context, cf clientFactory) discovery.Deps {
	deps := discovery.Deps{
		Lifecycle:       ctx,
		Background:      &a.wg,
		Logger:          slog.Default(),
		Transport:       cf.roundTripper(),
		ProviderTimeout: discoveryHTTPTimeout,
		ChartTimeout:    chartHTTPTimeout,
		Pool:            a.pool,
		Redis:           a.redisClient,
		CacheOps:        cacheOps(),
		ClientEvents:    a.discoveryClientEvents(),
		Providers:       discoveryProviderSettings(a.cfg),
		Search:          discoverySearchSettings(a.cfg),
		Unreleased:      discovery.UnreleasedSettings{ServeFromCache: a.cfg.UnreleasedSource == "cache", Tuning: a.unreleasedConfig()},
	}
	if a.eventTap != nil {
		deps.Activity = a.eventTap
	}
	return deps
}

func discoveryProviderSettings(cfg *config.Config) discovery.ProviderSettings {
	return discovery.ProviderSettings{
		MusicBrainzUserAgent: cfg.MusicBrainzUserAgent,
		LastFMAPIKey:         cfg.LastFMAPIKey,
		DiscogsToken:         cfg.DiscogsToken,
		FanartTVAPIKey:       cfg.FanartTVAPIKey,
		GeniusAccessToken:    cfg.GeniusAccessToken,
		TheAudioDBAPIKey:     cfg.TheAudioDBAPIKey,
		GoogleSheetsAPIKey:   cfg.GoogleSheetsAPIKey,
		TrackerHubSheetID:    cfg.TrackerHubSheetID,
		AppleMusicEnabled:    cfg.AppleMusicEnabled,
		SpotifyEnabled:       cfg.SpotifyEnabled,
		SoundCloudEnabled:    cfg.SoundCloudEnabled,
		AmazonMusicEnabled:   cfg.AmazonMusicEnabled,
		YouTubeMusicEnabled:  cfg.YtMusicEnabled,
		SearchAllowlist:      cfg.DiscoverySearchProviders,
	}
}

func discoverySearchSettings(cfg *config.Config) discovery.SearchSettings {
	return discovery.SearchSettings{
		Deadline:                cfg.DiscoverySearchDeadline,
		TailDemotion:            cfg.TailDemotionEnabled,
		CrossKindProminence:     cfg.CrossKindProminenceEnabled,
		Exploration:             cfg.ExplorationEnabled,
		ExplorationRate:         cfg.ExplorationRate,
		IdentityVerifyOnPersist: cfg.IdentityVerifyOnPersist,
		BehavioralRanking:       cfg.BehavioralRankingEnabled,
		ProviderCacheTTL:        cfg.DiscoveryProviderCacheTTL,
		MusicBrainzCacheTTL:     cfg.DiscoveryMusicBrainzCacheTTL,
	}
}

func (a *App) unreleasedConfig() discovery.UnreleasedConfig {
	return discovery.UnreleasedConfig{
		MaxPages:          a.cfg.GeniusUnreleasedMaxPages,
		MaxDetails:        a.cfg.GeniusUnreleasedMaxDetails,
		DetailConcurrency: a.cfg.GeniusUnreleasedDetailConcurrency,
		TTL:               a.cfg.GeniusUnreleasedTTL,
		EmptyTTL:          a.cfg.GeniusUnreleasedEmptyTTL,
		Wait:              a.cfg.GeniusUnreleasedWait,
		JobTimeout:        a.cfg.GeniusUnreleasedJobTimeout,
	}
}

func (a *App) discoveryClientEvents() *telemetry.ClientEvents {
	if !telemetry.Enabled(os.Getenv) {
		return nil
	}
	return telemetry.NewClientEvents(a.cfg.TelemetryUserKey, otel.GetLoggerProvider().Logger(telemetry.ScopeName))
}

func (a *App) startDiscoveryBackgroundJobs(ctx context.Context, disc *discovery.Discovery) {
	a.startUnreleasedRefresh(disc.UnreleasedRefresh)
	if a.cfg.BehavioralRankingEnabled {
		a.startEveryInstanceTicker(ctx, jobBehavioralRankingRefresh, 30*time.Minute, func(ctx context.Context) error {
			if err := disc.Search.RefreshBehavioralScores(ctx); err != nil {
				slog.WarnContext(ctx, string(jobBehavioralRankingRefresh)+" failed", "error", err)
				return err
			}
			return nil
		})
	}
	a.startCorpusRefresh(ctx, disc.Events)
	a.startDiscographyPrune(ctx, disc.Events)
	a.startVocabularyRefresh(disc.Vocabulary, disc.Charts)
}

const cacheOpsVarName = "discovery_cache_ops"

func cacheOps() *expvar.Map {
	ops, ok := expvar.Get(cacheOpsVarName).(*expvar.Map)
	if !ok {
		ops = expvar.NewMap(cacheOpsVarName)
	}
	return ops
}

func cacheSignal() *discovery.CacheSignal {
	return discovery.NewCacheSignal(cacheOps(), time.Now)
}

func cacheSignalOption() discovery.CacheOption {
	return discovery.WithCacheSignal(cacheSignal())
}
