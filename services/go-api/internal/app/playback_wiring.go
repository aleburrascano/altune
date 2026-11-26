package app

import (
	"altune/go-api/internal/app/bridges/playbackfromlibrary"
	"altune/go-api/internal/library"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/config"
	"context"
	"fmt"
	"time"
)

type playbackWiring struct {
	handler                 *playback.QueueHandler
	forgetDeletedIdentities *playback.DeletedUserDataCleanup
}

func (a *App) wirePlayback(ctx context.Context, trackRepo *library.PostgresTrackRepository) (playbackWiring, error) {
	metrics := playback.NewExpvarPlaybackMetrics()
	queueStateRepo := playback.NewPostgresQueueStateRepository(a.pool, metrics, clock.Now)
	queueSvc, err := newQueueService(queueStateRepo, trackRepo, metrics, a.cfg.PlaybackNowPlayingLookupTimeout)
	if err != nil {
		return playbackWiring{}, err
	}
	queueHandler, err := newQueueHandler(queueSvc, metrics, queueRateLimit(a.cfg))
	if err != nil {
		return playbackWiring{}, err
	}
	return playbackWiring{
		handler: queueHandler,
		forgetDeletedIdentities: playback.NewErasureSweeper(
			playback.NewPostgresDeletedIdentityRepository(a.pool), queueSvc,
			metrics, queueStateRepo, a.cfg.PlaybackErasureScanTimeout),
	}, nil
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
) (*playback.QueueService, error) {
	nowPlayingReader, err := playback.NewNowPlayingReader(playbackfromlibrary.NewNowPlayingTracks(trackRepo),
		metrics, clock.Now, lookupTimeout)
	if err != nil {
		return nil, fmt.Errorf("build the now-playing reader: %w", err)
	}
	return playback.NewQueueService(queueStateRepo, nowPlayingReader, clock.Now)
}

func newQueueHandler(queueSvc *playback.QueueService, metrics playbackMetricsSink, limit playback.QueueSaveLimit) (*playback.QueueHandler, error) {
	return playback.NewQueueHandler(queueSvc, limit, clock.Now, metrics)
}

func queueRateLimit(cfg *config.Config) playback.QueueSaveLimit {
	return playback.QueueSaveLimit{Every: cfg.PlaybackQueueRateEvery, Burst: cfg.PlaybackQueueRateBurst}
}
