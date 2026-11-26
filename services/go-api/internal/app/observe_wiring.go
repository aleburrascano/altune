package app

import (
	"altune/go-api/internal/app/bridges/monitoringfromdownload"
	"altune/go-api/internal/app/bridges/monitoringfromsearch"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/app/livemetrics"
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/search"
	"context"

	"github.com/go-chi/chi/v5"
)

func (a *App) wireObserve(ctx context.Context, r *chi.Mux, verifier auth.TokenVerifier, tap *monitoring.EventTap) {
	a.startObserveSources(ctx, tap)
	deps := monitoring.Deps{
		Health:      a.dependencyHealth,
		Logs:        a.logRing,
		Events:      a.eventFeed,
		Eval:        a.evalMeter,
		Acquisition: a.observeAcquisition(),
		LiveMetrics: a.liveMetrics,
		Discography: monitoringfromsearch.NewDiscographyQualityReader(search.NewPostgresEventStore(a.pool)),
		Shutdown:    a.lifecycleDone,
	}
	mountObserve(r, verifier, a.cfg.OverseerPrincipalID, monitoring.NewObserveHandler(deps))
}

func (a *App) startObserveSources(ctx context.Context, tap *monitoring.EventTap) {
	a.eventFeed = monitoring.NewEventFeed()
	a.eventFeed.Start(ctx, tap)
	a.evalMeter = monitoring.NewEvalMeter(0, a.evalMeterRunner()).
		WithLeadership(a.jobs.LeaderContext)
	a.jobs.WhenLeader(jobs.EvalMeter, a.evalMeter.Start)
}

func mountObserve(r chi.Router, verifier auth.TokenVerifier, principalID string, h *monitoring.Handler) {
	r.Route("/observe", func(or chi.Router) {
		or.Use(monitoring.NoStoreAndNosniff)
		or.Use(authMiddleware(verifier))
		or.Use(monitoring.Gate(principalID))
		h.Register(or)
	})
}

func (a *App) observeAcquisition() monitoring.AcquisitionReader {
	if a.scheduler == nil {
		return nil
	}
	return monitoringfromdownload.NewAcquisitionReader(a.scheduler)
}

func (a *App) liveMetrics() monitoring.LiveMetrics {
	return livemetrics.Live(livemetrics.Sources{
		Latency:      a.latency,
		Pool:         a.pool,
		Redis:        a.redisClient,
		Leader:       a.jobs.LeaderCounters(),
		EventBus:     a.eventBus,
		EventTap:     a.eventTap,
		EventFeed:    a.eventFeed,
		AlertMonitor: a.alertMonitor,
		Background:   a.background,
	})
}

func (a *App) evalMeterRunner() monitoring.EvalMeterRunner {
	return monitoringfromsearch.NewEvalMeterRunner(a.buildEvalRunner())
}
