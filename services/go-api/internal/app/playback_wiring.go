package app

import (
	"altune/go-api/internal/catalog/adapters/persistence"
	"altune/go-api/internal/playback/adapters/catalogbridge"
	"altune/go-api/internal/playback/ports"
	"log/slog"

	playbackHandler "altune/go-api/internal/playback/adapters/handler"
	playbackMetrics "altune/go-api/internal/playback/adapters/metrics"
	playbackPersistence "altune/go-api/internal/playback/adapters/persistence"
	playbackService "altune/go-api/internal/playback/service"
)

type playbackWiring struct {
	handler                 *playbackHandler.QueueHandler
	forgetDeletedIdentities *playbackService.ForgetDeletedIdentitiesService
}

func (a *App) wirePlayback(trackRepo *persistence.PgxTrackRepository) playbackWiring {
	metrics := playbackMetrics.NewExpvarPlaybackMetrics()
	queueStateRepo := playbackPersistence.NewPgxQueueStateRepository(a.pool, playbackPersistence.WithQueueStateMetrics(metrics))
	queueSvc := newQueueService(queueStateRepo, trackRepo, metrics, a.cfg.HasNowPlayingEnrichment())
	return playbackWiring{
		handler: newQueueHandler(queueSvc, metrics),
		forgetDeletedIdentities: playbackService.NewForgetDeletedIdentitiesService(
			playbackPersistence.NewPgxDeletedIdentityRepository(a.pool), queueSvc,
			playbackService.WithErasureSweepMetrics(metrics)),
	}
}

type playbackMetricsSink interface {
	ports.EnrichmentMetrics
	ports.RateLimitMetrics
}

func newQueueService(
	queueStateRepo ports.QueueStateRepository,
	trackRepo *persistence.PgxTrackRepository,
	metrics playbackMetricsSink,
	enrichmentEnabled bool,
) *playbackService.QueueService {
	if !enrichmentEnabled {
		slog.Info("playback: now-playing enrichment disabled via PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED")
	}
	nowPlayingReader := catalogbridge.NewNowPlayingReader(trackRepo, catalogbridge.WithNowPlayingMetrics(metrics))
	return playbackService.NewQueueService(queueStateRepo, nowPlayingReader,
		playbackService.WithNowPlayingEnrichment(enrichmentEnabled))
}

func newQueueHandler(queueSvc *playbackService.QueueService, metrics playbackMetricsSink) *playbackHandler.QueueHandler {
	return playbackHandler.NewQueueHandler(queueSvc, playbackHandler.WithQueueStateRateLimitMetrics(metrics))
}
