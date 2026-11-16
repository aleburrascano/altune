package app

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/config"
	"context"
	"log/slog"

	playbackHandler "altune/go-api/internal/playback/adapters/handler"
	playbackErasure "altune/go-api/internal/playback/service/erasure"
	playbackQueue "altune/go-api/internal/playback/service/queue"
)

type playbackWiring struct {
	handler                 *playbackHandler.QueueHandler
	forgetDeletedIdentities *playbackErasure.Sweeper
}

func (a *App) wirePlayback(ctx context.Context, trackRepo *catalog.PgxTrackRepository) playbackWiring {
	metrics := playback.NewExpvarPlaybackMetrics()
	queueStateRepo := playback.NewPgxQueueStateRepository(a.pool, playback.WithQueueStateMetrics(metrics))
	queueSvc := newQueueService(ctx, queueStateRepo, trackRepo, metrics, a.cfg.HasNowPlayingEnrichment())
	return playbackWiring{
		handler: newQueueHandler(queueSvc, metrics, queueRateLimit(a.cfg)),
		forgetDeletedIdentities: playbackErasure.NewSweeper(
			playback.NewPgxDeletedIdentityRepository(a.pool), queueSvc,
			playbackErasure.WithSweepRunBudget(a.cfg.PlaybackErasureScanTimeout),
			playbackErasure.WithErasureSweepMetrics(metrics),
			playbackErasure.WithErasedStateReaper(queueStateRepo)),
	}
}

type playbackMetricsSink interface {
	playback.EnrichmentMetrics
	playback.RateLimitMetrics
}

func newQueueService(
	ctx context.Context,
	queueStateRepo playback.QueueStateRepository,
	trackRepo *catalog.PgxTrackRepository,
	metrics playbackMetricsSink,
	enrichmentEnabled bool,
) *playbackQueue.Service {
	if !enrichmentEnabled {
		slog.InfoContext(ctx, "playback: now-playing enrichment disabled via PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED")
	}
	nowPlayingReader := playback.NewNowPlayingReader(trackRepo, playback.WithNowPlayingMetrics(metrics))
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
