package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/leader"
	"altune/go-api/internal/shared/redis"
	"altune/go-api/internal/shared/reqmetrics"
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
		Discography: discovery.NewPgxEventStore(a.pool),
		Shutdown:    a.lifecycleDone,
	}
	mountObserve(r, verifier, a.cfg.OverseerPrincipalID, observe.NewObserveHandler(deps))
}

func (a *App) startObserveSources(ctx context.Context, tap *observe.EventTap) {
	a.eventFeed = observe.NewEventFeed()
	a.eventFeed.Start(ctx, tap)
	a.evalMeter = observe.NewEvalMeter(a.cfg.EvalMeterEnabled, 0, a.evalMeterRunner()).
		WithLeadership(a.leaderContext)
	a.whenLeaderUnlessDisabled(jobEvalMeter, a.evalMeter.Start)
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
	return a.scheduler
}

func liveMetricsSnapshot() observe.LiveMetrics {
	return observe.LiveMetrics{
		"auth":      auth.ReadSnapshot(),
		"catalog":   catalog.ReadSnapshot(),
		"playback":  playback.ReadSnapshot(),
		"providers": discovery.ReadProviderMetricsSnapshot(),
		"latency":   reqmetrics.ReadSnapshot(),
	}
}

type electionCounters interface{ Counters() leader.Counters }

type eventBusStats struct {
	Dropped uint64 `json:"dropped_total"`
}

type eventTapStats struct {
	TapDropped  uint64 `json:"tap_dropped_total"`
	FeedDropped uint64 `json:"feed_dropped_total"`
}

func (a *App) liveMetrics() observe.LiveMetrics {
	m := liveMetricsSnapshot()
	m["db_pool"] = database.ReadPoolStats(a.pool)
	m["redis_pool"] = redis.ReadPoolStats(a.redisClient)
	var counters leader.Counters
	if e, ok := a.election.(electionCounters); ok {
		counters = e.Counters()
	}
	m["leader"] = counters
	m["event_bus"] = a.busStats()
	if a.alertMonitor != nil {
		m["alert_monitor"] = a.alertMonitor.Status()
	}
	m["event_tap"] = a.tapStats()
	return m
}

func (a *App) busStats() eventBusStats {
	var bus eventBusStats
	if a.eventBus != nil {
		bus.Dropped = a.eventBus.Dropped()
	}
	return bus
}

func (a *App) tapStats() eventTapStats {
	var tapStats eventTapStats
	if a.eventTap != nil {
		tapStats.TapDropped = a.eventTap.Dropped()
	}
	if a.eventFeed != nil {
		tapStats.FeedDropped = a.eventFeed.Dropped()
	}
	return tapStats
}

func (a *App) evalMeterRunner() observe.EvalMeterRunner {
	return wrapEvalRunner(a.buildEvalRunner())
}

func wrapEvalRunner(run EvalRunner) observe.EvalMeterRunner {
	if run == nil {
		return nil
	}
	return func(ctx context.Context) (observe.EvalMeterResult, error) {
		res, err := run(ctx)
		if err != nil {
			return observe.EvalMeterResult{}, err
		}
		return meterResult(res), nil
	}
}

func meterResult(res EvalResult) observe.EvalMeterResult {
	queries := make([]observe.EvalMeterQueryResult, len(res.Queries))
	for i, q := range res.Queries {
		queries[i] = observe.EvalMeterQueryResult{
			Query:    q.Query,
			Expect:   q.Expect,
			Passed:   q.Passed,
			Position: q.Position,
		}
	}
	return observe.EvalMeterResult{
		Score:     res.Score,
		Baseline:  res.Baseline,
		Regressed: res.Regressed,
		Errored:   res.Errored,
		Queries:   queries,
	}
}
