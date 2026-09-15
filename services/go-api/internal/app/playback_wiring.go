package app

import (
	"altune/go-api/internal/catalog/adapters/persistence"
	"altune/go-api/internal/playback/adapters/catalogbridge"
	"altune/go-api/internal/playback/ports"

	playbackHandler "altune/go-api/internal/playback/adapters/handler"
	playbackMetrics "altune/go-api/internal/playback/adapters/metrics"
	playbackPersistence "altune/go-api/internal/playback/adapters/persistence"
	playbackService "altune/go-api/internal/playback/service"
)

// wirePlayback builds the queue handler with playback's degradation counters
// wired to the expvar adapter read by GET /admin/metrics/live.
func (a *App) wirePlayback(trackRepo *persistence.PgxTrackRepository) *playbackHandler.QueueHandler {
	metrics := playbackMetrics.NewExpvarPlaybackMetrics()
	queueStateRepo := playbackPersistence.NewPgxQueueStateRepository(a.pool, playbackPersistence.WithQueueStateMetrics(metrics))
	return newQueueHandler(queueStateRepo, trackRepo, metrics)
}

// newQueueHandler assembles the queue service over a queue-state store and the
// catalog-backed now-playing reader, both reporting into the same metrics sink.
func newQueueHandler(
	queueStateRepo ports.QueueStateRepository,
	trackRepo *persistence.PgxTrackRepository,
	metrics ports.PlaybackMetrics,
) *playbackHandler.QueueHandler {
	nowPlayingReader := catalogbridge.NewNowPlayingReader(trackRepo, catalogbridge.WithNowPlayingMetrics(metrics))
	queueSvc := playbackService.NewQueueService(queueStateRepo, nowPlayingReader)
	return playbackHandler.NewQueueHandler(queueSvc)
}
