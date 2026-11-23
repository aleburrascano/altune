package app

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/app/observeacquisition"
	"altune/go-api/internal/app/observediscovery"
	"altune/go-api/internal/app/observemetrics"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"context"

	"github.com/go-chi/chi/v5"
)

func (a *App) wireObserve(ctx context.Context, r *chi.Mux, verifier auth.TokenVerifier, tap *observe.EventTap) {
	a.startObserveSources(ctx, tap)
	deps := observe.Deps{
		Health:      a.dependencyHealth,
		Logs:        a.logRing,
		Events:      a.eventFeed,
		Eval:        a.evalMeter,
		Acquisition: a.observeAcquisition(),
		LiveMetrics: a.liveMetrics,
		Discography: observediscovery.NewDiscographyQualityReader(discovery.NewPgxEventStore(a.pool)),
		Shutdown:    a.lifecycleDone,
	}
	mountObserve(r, verifier, a.cfg.OverseerPrincipalID, observe.NewObserveHandler(deps))
}

func (a *App) startObserveSources(ctx context.Context, tap *observe.EventTap) {
	a.eventFeed = observe.NewEventFeed()
	a.eventFeed.Start(ctx, tap)
	a.evalMeter = observe.NewEvalMeter(0, a.evalMeterRunner()).
		WithLeadership(a.jobs.LeaderContext)
	a.jobs.WhenLeader(jobs.EvalMeter, a.evalMeter.Start)
}

func mountObserve(r chi.Router, verifier auth.TokenVerifier, principalID string, h *observe.Handler) {
	r.Route("/observe", func(or chi.Router) {
		or.Use(observe.NoStoreAndNosniff)
		or.Use(authMiddleware(verifier))
		or.Use(observe.Gate(principalID))
		h.Register(or)
	})
}

func (a *App) observeAcquisition() observe.AcquisitionReader {
	if a.scheduler == nil {
		return nil
	}
	return observeacquisition.NewAcquisitionReader(a.scheduler)
}

func (a *App) liveMetrics() observe.LiveMetrics {
	return observemetrics.Live(observemetrics.Sources{
		Latency:      a.latency,
		Pool:         a.pool,
		Redis:        a.redisClient,
		Leader:       a.jobs.LeaderCounters(),
		EventBus:     a.eventBus,
		EventTap:     a.eventTap,
		EventFeed:    a.eventFeed,
		AlertMonitor: a.alertMonitor,
	})
}

func (a *App) evalMeterRunner() observe.EvalMeterRunner {
	return observediscovery.NewEvalMeterRunner(a.buildEvalRunner())
}
