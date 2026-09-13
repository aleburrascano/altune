package app

import (
	"altune/go-api/internal/catalog/adapters/persistence"
	"altune/go-api/internal/playback/adapters/catalogbridge"

	playbackHandler "altune/go-api/internal/playback/adapters/handler"
	playbackMetrics "altune/go-api/internal/playback/adapters/metrics"
	playbackPersistence "altune/go-api/internal/playback/adapters/persistence"
	playbackService "altune/go-api/internal/playback/service"
)

func (a *App) wirePlayback(trackRepo *persistence.PgxTrackRepository) *playbackHandler.QueueHandler {
	metrics := playbackMetrics.NewExpvarPlaybackMetrics()
	queueStateRepo := playbackPersistence.NewPgxQueueStateRepository(a.pool, playbackPersistence.WithQueueStateMetrics(metrics))
	nowPlayingReader := catalogbridge.NewNowPlayingReader(trackRepo, catalogbridge.WithNowPlayingMetrics(metrics))
	queueSvc := playbackService.NewQueueService(queueStateRepo, nowPlayingReader)
	return playbackHandler.NewQueueHandler(queueSvc)
}
