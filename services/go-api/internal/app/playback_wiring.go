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

// playbackWiring is what the composition root keeps of the playback module: the
// HTTP surface, and the erasure sweep the background scheduler drives.
type playbackWiring struct {
	handler                 *playbackHandler.QueueHandler
	forgetDeletedIdentities *playbackService.ForgetDeletedIdentitiesService
}

// wirePlayback builds the queue handler with playback's degradation counters
// wired to the expvar adapter read by GET /admin/metrics/live, and the erasure
// sweep over the same queue service, so an erasure it drives leaves the same
// audit record as the self-service route.
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

// playbackMetricsSink is the composition root's view of the one expvar adapter:
// the union of the ports its consumers each depend on alone.
type playbackMetricsSink interface {
	ports.EnrichmentMetrics
	ports.RateLimitMetrics
}

// newQueueService assembles the queue service over a queue-state store and the
// catalog-backed now-playing reader; metrics is the enrichment sink of the
// reader, the store arriving with its own already wired.
// enrichmentEnabled is the PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED kill switch:
// when false, resume never calls the reader.
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

// newQueueHandler serves the queue service over HTTP, with metrics as the
// handler's rate-limit sink.
func newQueueHandler(queueSvc *playbackService.QueueService, metrics playbackMetricsSink) *playbackHandler.QueueHandler {
	return playbackHandler.NewQueueHandler(queueSvc, playbackHandler.WithQueueStateRateLimitMetrics(metrics))
}
