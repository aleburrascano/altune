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

// wirePlayback builds the queue handler with playback's degradation counters
// wired to the expvar adapter read by GET /admin/metrics/live.
func (a *App) wirePlayback(trackRepo *persistence.PgxTrackRepository) *playbackHandler.QueueHandler {
	metrics := playbackMetrics.NewExpvarPlaybackMetrics()
	queueStateRepo := playbackPersistence.NewPgxQueueStateRepository(a.pool, playbackPersistence.WithQueueStateMetrics(metrics))
	return newQueueHandler(queueStateRepo, trackRepo, metrics, a.cfg.HasNowPlayingEnrichment())
}

// playbackMetricsSink is the composition root's view of the one expvar adapter:
// the union of the ports its consumers each depend on alone.
type playbackMetricsSink interface {
	ports.EnrichmentMetrics
	ports.RateLimitMetrics
}

// newQueueHandler assembles the queue service over a queue-state store and the
// catalog-backed now-playing reader; metrics is the enrichment sink of the
// reader and the rate-limit sink of the handler, the store arriving with its
// own already wired.
// enrichmentEnabled is the PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED kill switch:
// when false, resume never calls the reader.
func newQueueHandler(
	queueStateRepo ports.QueueStateRepository,
	trackRepo *persistence.PgxTrackRepository,
	metrics playbackMetricsSink,
	enrichmentEnabled bool,
) *playbackHandler.QueueHandler {
	if !enrichmentEnabled {
		slog.Info("playback: now-playing enrichment disabled via PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED")
	}
	nowPlayingReader := catalogbridge.NewNowPlayingReader(trackRepo, catalogbridge.WithNowPlayingMetrics(metrics))
	queueSvc := playbackService.NewQueueService(queueStateRepo, nowPlayingReader,
		playbackService.WithNowPlayingEnrichment(enrichmentEnabled))
	return playbackHandler.NewQueueHandler(queueSvc, playbackHandler.WithQueueStateRateLimitMetrics(metrics))
}
