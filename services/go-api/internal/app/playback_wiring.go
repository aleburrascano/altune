package app

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/config"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"fmt"
	"time"
)

const nowPlayingLookupTimeout = 3 * time.Second

type playbackWiring struct {
	handler                 *playback.QueueHandler
	forgetDeletedIdentities *playback.ErasureSweeper
}

func (a *App) wirePlayback(ctx context.Context, trackRepo *catalog.PgxTrackRepository) playbackWiring {
	metrics := playback.NewExpvarPlaybackMetrics()
	queueStateRepo := playback.NewPgxQueueStateRepository(a.pool, playback.WithQueueStateMetrics(metrics))
	queueSvc := newQueueService(queueStateRepo, trackRepo, metrics)
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
) *playback.QueueService {
	nowPlayingReader := playback.NewNowPlayingReader(catalogTrackSource{tracks: trackRepo},
		playback.WithNowPlayingMetrics(metrics),
		playback.WithNowPlayingLookupTimeout(nowPlayingLookupTimeout),
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

type catalogTracks interface {
	GetByID(ctx context.Context, id catalog.TrackId, userId shared.UserId) (*catalog.Track, error)
}

type catalogTrackSource struct {
	tracks catalogTracks
}

func (s catalogTrackSource) NowPlayingTrack(ctx context.Context, userId shared.UserId, id shared.TrackId) (*playback.NowPlayingTrack, error) {
	track, err := s.tracks.GetByID(ctx, id, userId)
	if errors.Is(err, catalog.ErrDBTransient) {
		return nil, fmt.Errorf("%w: %w", playback.ErrTrackSourceUnavailable, err)
	}
	if err != nil || track == nil {
		return nil, err
	}
	return &playback.NowPlayingTrack{
		Id:                track.ID.String(),
		Title:             track.Title,
		Artist:            track.Artist,
		ArtworkURL:        track.ArtworkURL,
		DurationSeconds:   track.DurationSeconds,
		AcquisitionStatus: track.AcquisitionStatus.String(),
	}, nil
}
