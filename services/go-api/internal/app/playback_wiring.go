package app

import (
	"altune/go-api/internal/app/playbackcatalog"
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/config"
	"context"
	"time"
)

type playbackWiring struct {
	handler                 *playback.QueueHandler
	forgetDeletedIdentities *playback.ErasureSweeper
}

func (a *App) wirePlayback(ctx context.Context, trackRepo *catalog.PgxTrackRepository) playbackWiring {
	metrics := playback.NewExpvarPlaybackMetrics()
	queueStateRepo := playback.NewPgxQueueStateRepository(a.pool, playback.WithQueueStateMetrics(metrics))
	queueSvc := newQueueService(queueStateRepo, trackRepo, metrics, a.cfg.PlaybackNowPlayingLookupTimeout)
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
	queueStateRepo playback.QueueStateRepository,
	trackRepo *catalog.PgxTrackRepository,
	metrics playbackMetricsSink,
	lookupTimeout time.Duration,
) *playback.QueueService {
	nowPlayingReader := playback.NewNowPlayingReader(playbackcatalog.NewNowPlayingTracks(trackRepo),
		playback.WithNowPlayingMetrics(metrics),
		playback.WithNowPlayingLookupTimeout(lookupTimeout),
		playback.WithNowPlayingClock(time.Now))
	return playback.NewQueueService(queueStateRepo, nowPlayingReader)
}

func newQueueHandler(queueSvc *playback.QueueService, metrics playbackMetricsSink, limit playback.QueueStateRateLimit) *playback.QueueHandler {
	return playback.NewQueueHandler(queueSvc,
		playback.WithQueueStateRateLimit(limit),
		playback.WithQueueStateRateLimitMetrics(metrics),
		playback.WithQueueStateClock(time.Now))
}

func queueRateLimit(cfg *config.Config) playback.QueueStateRateLimit {
	return playback.QueueStateRateLimit{Every: cfg.PlaybackQueueRateEvery, Burst: cfg.PlaybackQueueRateBurst}
}
