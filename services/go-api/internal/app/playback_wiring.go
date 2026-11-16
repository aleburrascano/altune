package app

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/config"
	"context"
	"log/slog"
)

type playbackWiring struct {
	handler                 *playback.QueueHandler
	forgetDeletedIdentities *playback.ErasureSweeper
}

func (a *App) wirePlayback(ctx context.Context, trackRepo *catalog.PgxTrackRepository) playbackWiring {
	metrics := playback.NewExpvarPlaybackMetrics()
	queueStateRepo := playback.NewPgxQueueStateRepository(a.pool, playback.WithQueueStateMetrics(metrics))
	queueSvc := newQueueService(ctx, queueStateRepo, trackRepo, metrics, a.cfg.HasNowPlayingEnrichment())
	return playbackWiring{
		handler: newQueueHandler(queueSvc, metrics, queueRateLimit(a.cfg)),
		forgetDeletedIdentities: playback.NewErasureSweeper(
			playback.NewPgxDeletedIdentityRepository(a.pool), queueSvc,
			playback.WithSweepRunBudget(a.cfg.PlaybackErasureScanTimeout),
			playback.WithErasureSweepMetrics(metrics),
			playback.WithErasedStateReaper(queueStateRepo)),
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
) *playback.QueueService {
	if !enrichmentEnabled {
		slog.InfoContext(ctx, "playback: now-playing enrichment disabled via PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED")
	}
	nowPlayingReader := playback.NewNowPlayingReader(trackRepo, playback.WithNowPlayingMetrics(metrics))
	return playback.NewQueueService(queueStateRepo, nowPlayingReader,
		playback.WithNowPlayingEnrichment(enrichmentEnabled))
}

func newQueueHandler(queueSvc *playback.QueueService, metrics playbackMetricsSink, limit playback.QueueStateRateLimit) *playback.QueueHandler {
	return playback.NewQueueHandler(queueSvc,
		playback.WithQueueStateRateLimit(limit),
		playback.WithQueueStateRateLimitMetrics(metrics))
}

func queueRateLimit(cfg *config.Config) playback.QueueStateRateLimit {
	return playback.QueueStateRateLimit{Every: cfg.PlaybackQueueRateEvery, Burst: cfg.PlaybackQueueRateBurst}
}
