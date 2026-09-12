package app

import (
	"altune/go-api/internal/catalog/adapters/persistence"
	"altune/go-api/internal/playback/adapters/catalogbridge"

	playbackHandler "altune/go-api/internal/playback/adapters/handler"
	playbackPersistence "altune/go-api/internal/playback/adapters/persistence"
	playbackService "altune/go-api/internal/playback/service"
)

func (a *App) wirePlayback(trackRepo *persistence.PgxTrackRepository) *playbackHandler.QueueHandler {
	queueStateRepo := playbackPersistence.NewPgxQueueStateRepository(a.pool)
	nowPlayingReader := catalogbridge.NewNowPlayingReader(trackRepo)
	queueSvc := playbackService.NewQueueService(queueStateRepo, nowPlayingReader)
	return playbackHandler.NewQueueHandler(queueSvc)
}
