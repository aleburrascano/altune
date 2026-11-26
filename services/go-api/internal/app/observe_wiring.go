package app

import (
	"altune/go-api/internal/app/bridges/monitoringfromdownload"
	"altune/go-api/internal/app/bridges/monitoringfromsearch"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/app/livemetrics"
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/database"
	"context"
	"fmt"

	"github.com/go-chi/chi/v5"
)

func (a *App) wireObserve(ctx context.Context, r *chi.Mux, verifier auth.TokenVerifier, tap *monitoring.EventTap) error {
	if err := a.startObserveSources(ctx, tap); err != nil {
		return err
	}
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
	mountObserve(r, verifier, a.cfg.OverseerPrincipalID, monitoring.NewObserveHandler(clock.Now, clock.NewTicker, deps))
	return nil
}

func (a *App) startObserveSources(ctx context.Context, tap *monitoring.EventTap) error {
	a.eventFeed = monitoring.NewEventFeed()
	a.eventFeed.Start(ctx, tap)
	meter, err := NewEvalMeter(&a.jobs, a.evalMeterRunner())
	if err != nil {
		return err
	}
	a.evalMeter = meter
	a.jobs.WhenLeader(jobs.EvalMeter, a.evalMeter.Start)
	return nil
}

func NewEvalMeter(runner *jobs.Runner, run monitoring.EvalMeterRunner) (*monitoring.EvalMeter, error) {
	meter, err := monitoring.NewEvalMeter(0, clock.Now, clock.NewTicker, run, runner.LeaderContextFor(jobs.EvalMeter))
	if err != nil {
		return nil, fmt.Errorf("eval meter: %w", err)
	}
	return meter, nil
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
	if !a.acquisitionOn {
		return nil
	}
	return monitoringfromdownload.NewAcquisitionReader(a.scheduler)
}

func (a *App) liveMetrics() monitoring.LiveMetrics {
	return livemetrics.Live(livemetrics.Sources{
		Latency:      a.latency,
		PoolStats:    func() database.PoolStats { return database.ReadPoolStats(a.pool) },
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
