package app

import (
	"altune/go-api/internal/app/bridges/playbackfromlibrary"
	"altune/go-api/internal/library"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/config"
	"context"
	"time"
)

type playbackWiring struct {
	handler                 *playback.QueueHandler
	forgetDeletedIdentities *playback.DeletedUserDataCleanup
}

func (a *App) wirePlayback(ctx context.Context, trackRepo *library.PostgresTrackRepository) playbackWiring {
	metrics := playback.NewExpvarPlaybackMetrics()
	queueStateRepo := playback.NewPostgresQueueStateRepository(a.pool, playback.WithQueueStateMetrics(metrics))
	queueSvc := newQueueService(queueStateRepo, trackRepo, metrics, a.cfg.PlaybackNowPlayingLookupTimeout)
	return playbackWiring{
		handler: newQueueHandler(queueSvc, metrics, queueRateLimit(a.cfg)),
		forgetDeletedIdentities: playback.NewErasureSweeper(
			playback.NewPostgresDeletedIdentityRepository(a.pool), queueSvc,
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
	trackRepo *library.PostgresTrackRepository,
	metrics playbackMetricsSink,
	lookupTimeout time.Duration,
) *playback.QueueService {
	nowPlayingReader := playback.NewNowPlayingReader(playbackfromlibrary.NewNowPlayingTracks(trackRepo),
		playback.WithNowPlayingMetrics(metrics),
		playback.WithNowPlayingLookupTimeout(lookupTimeout),
		playback.WithNowPlayingClock(time.Now))
	return playback.NewQueueService(queueStateRepo, nowPlayingReader)
}

func newQueueHandler(queueSvc *playback.QueueService, metrics playbackMetricsSink, limit playback.QueueSaveLimit) *playback.QueueHandler {
	return playback.NewQueueHandler(queueSvc,
		playback.WithQueueSaveLimit(limit),
		playback.WithQueueStateRateLimitMetrics(metrics),
		playback.WithQueueStateClock(time.Now))
}

func queueRateLimit(cfg *config.Config) playback.QueueSaveLimit {
	return playback.QueueSaveLimit{Every: cfg.PlaybackQueueRateEvery, Burst: cfg.PlaybackQueueRateBurst}
}
