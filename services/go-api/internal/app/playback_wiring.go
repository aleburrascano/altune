package app

import (
	"altune/go-api/internal/catalog/adapters/persistence"
	"altune/go-api/internal/playback/adapters/catalogbridge"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared/config"
	"context"
	"log/slog"

	playbackHandler "altune/go-api/internal/playback/adapters/handler"
	playbackMetrics "altune/go-api/internal/playback/adapters/metrics"
	playbackPersistence "altune/go-api/internal/playback/adapters/persistence"
	playbackErasure "altune/go-api/internal/playback/service/erasure"
	playbackQueue "altune/go-api/internal/playback/service/queue"
)

type playbackWiring struct {
	handler                 *playbackHandler.QueueHandler
	forgetDeletedIdentities *playbackErasure.Sweeper
}

func (a *App) wirePlayback(ctx context.Context, trackRepo *persistence.PgxTrackRepository) playbackWiring {
	metrics := playbackMetrics.NewExpvarPlaybackMetrics()
	queueStateRepo := playbackPersistence.NewPgxQueueStateRepository(a.pool, playbackPersistence.WithQueueStateMetrics(metrics))
	queueSvc := newQueueService(ctx, queueStateRepo, trackRepo, metrics, a.cfg.HasNowPlayingEnrichment())
	return playbackWiring{
		handler: newQueueHandler(queueSvc, metrics, queueRateLimit(a.cfg)),
		forgetDeletedIdentities: playbackErasure.NewSweeper(
			playbackPersistence.NewPgxDeletedIdentityRepository(a.pool), queueSvc,
			playbackErasure.WithSweepRunBudget(a.cfg.PlaybackErasureScanTimeout),
			playbackErasure.WithErasureSweepMetrics(metrics),
			playbackErasure.WithErasedStateReaper(queueStateRepo)),
	}
}

type playbackMetricsSink interface {
	ports.EnrichmentMetrics
	ports.RateLimitMetrics
}

func newQueueService(
	ctx context.Context,
	queueStateRepo ports.QueueStateRepository,
	trackRepo *persistence.PgxTrackRepository,
	metrics playbackMetricsSink,
	enrichmentEnabled bool,
) *playbackQueue.Service {
	if !enrichmentEnabled {
		slog.InfoContext(ctx, "playback: now-playing enrichment disabled via PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED")
	}
	nowPlayingReader := catalogbridge.NewNowPlayingReader(trackRepo, catalogbridge.WithNowPlayingMetrics(metrics))
	return playbackQueue.New(queueStateRepo, nowPlayingReader,
		playbackQueue.WithNowPlayingEnrichment(enrichmentEnabled))
}

func newQueueHandler(queueSvc *playbackQueue.Service, metrics playbackMetricsSink, limit playbackHandler.QueueStateRateLimit) *playbackHandler.QueueHandler {
	return playbackHandler.NewQueueHandler(queueSvc,
		playbackHandler.WithQueueStateRateLimit(limit),
		playbackHandler.WithQueueStateRateLimitMetrics(metrics))
}

func queueRateLimit(cfg *config.Config) playbackHandler.QueueStateRateLimit {
	return playbackHandler.QueueStateRateLimit{Every: cfg.PlaybackQueueRateEvery, Burst: cfg.PlaybackQueueRateBurst}
}
