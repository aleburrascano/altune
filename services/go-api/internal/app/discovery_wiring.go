package app

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/app/providerhttp"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"expvar"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
)

type EvalHandles struct {
	Pool          *pgxpool.Pool
	Redis         *goredis.Client
	Transport     http.RoundTripper
	IdentityStore discovery.IdentityStore
}

func NewEvalDiscovery(ctx context.Context, cfg *config.Config, h EvalHandles) (*discovery.Discovery, error) {
	transport := h.Transport
	if transport == nil {
		transport = providerhttp.Live()
	}
	deps := configuredDiscoveryDeps(ctx, cfg, transport)
	deps.Background = &sync.WaitGroup{}
	deps.Pool = h.Pool
	deps.Redis = h.Redis
	deps.IdentityStore = h.IdentityStore
	return discovery.New(deps)
}

func (a *App) discoveryDeps(ctx context.Context, transport http.RoundTripper) discovery.Deps {
	deps := configuredDiscoveryDeps(ctx, a.cfg, transport)
	deps.Background = a.jobs.Background()
	deps.Pool = a.pool
	deps.Redis = a.redisClient
	deps.ClientEvents = a.discoveryClientEvents()
	if a.eventTap != nil {
		deps.Activity = a.eventTap
	}
	return deps
}

func configuredDiscoveryDeps(ctx context.Context, cfg *config.Config, transport http.RoundTripper) discovery.Deps {
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

func (a *App) discoveryClientEvents() *telemetry.ClientEvents {
	if !a.cfg.HasTelemetryExport() {
		return nil
	}
	return telemetry.NewClientEvents(a.cfg.TelemetryUserKey, otel.GetLoggerProvider().Logger(telemetry.ScopeName))
}

func (a *App) startDiscoveryBackgroundJobs(ctx context.Context, disc *discovery.Discovery) {
	a.startUnreleasedRefresh(disc.UnreleasedRefresh)
	if a.cfg.BehavioralRankingEnabled {
		a.jobs.EveryInstance(ctx, jobs.BehavioralRankingRefresh, 30*time.Minute, func(ctx context.Context) error {
			if err := disc.Search.RefreshBehavioralScores(ctx); err != nil {
				slog.WarnContext(ctx, string(jobs.BehavioralRankingRefresh)+" failed", "error", err)
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
