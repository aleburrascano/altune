package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/observe/evalmeter"
	"altune/go-api/internal/observe/eventtap"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/leader"
	"altune/go-api/internal/shared/redis"
	"altune/go-api/internal/shared/reqmetrics"
	"context"

	authmetrics "altune/go-api/internal/auth/adapters/metrics"
	catalogmetrics "altune/go-api/internal/catalog/adapters/metrics"
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"
	providermetrics "altune/go-api/internal/discovery/adapters/providermetrics"
	feedbackmetrics "altune/go-api/internal/feedback/adapters/metrics"
	observeHandler "altune/go-api/internal/observe/handler"
	playbackmetrics "altune/go-api/internal/playback/adapters/metrics"

	"github.com/go-chi/chi/v5"
)

func (a *App) wireObserve(ctx context.Context, r *chi.Mux, verifier auth.TokenVerifier, tap *eventtap.Tap) {
	a.startObserveSources(ctx, tap)
	deps := observeHandler.Deps{
		Health:      a.observeHealthProbe,
		Logs:        a.logRing,
		Events:      a.eventFeed,
		Eval:        a.evalMeter,
		Acquisition: a.observeAcquisition(),
		LiveMetrics: a.liveMetrics,
		Discography: discoveryPersistence.NewPgxEventStore(a.pool),
		Shutdown:    a.lifecycleDone,
	}
	mountObserve(r, verifier, observePrincipal(a.cfg), observeHandler.New(deps))
}

func (a *App) startObserveSources(ctx context.Context, tap *eventtap.Tap) {
	a.eventFeed = eventtap.NewFeed()
	a.eventFeed.Start(ctx, tap)
	a.evalMeter = evalmeter.New(a.cfg.EvalMeterEnabled, 0, a.evalMeterRunner()).
		WithLeadership(a.leaderContext)
	a.whenLeader(jobEvalMeter, a.evalMeter.Start)
}

func mountObserve(r chi.Router, verifier auth.TokenVerifier, principalID string, h *observeHandler.Handler) {
	r.Route("/observe", func(or chi.Router) {
		or.Use(observeHandler.NoStoreAndNosniff)
		or.Use(authMiddleware(verifier))
		or.Use(observeHandler.Gate(principalID))
		h.Register(or)
	})
}

func observePrincipal(cfg *config.Config) string {
	if cfg.OverseerPrincipalID != "" {
		return cfg.OverseerPrincipalID
	}
	return cfg.OperatorReadOnlyUserID
}

func (a *App) observeAcquisition() observeHandler.AcquisitionReader {
	if a.scheduler == nil {
		return nil
	}
	return a.scheduler
}

func liveMetricsSnapshot() observeHandler.LiveMetrics {
	return observeHandler.LiveMetrics{
		"auth":      authmetrics.ReadSnapshot(),
		"catalog":   catalogmetrics.ReadSnapshot(),
		"feedback":  feedbackmetrics.ReadSnapshot(),
		"playback":  playbackmetrics.ReadSnapshot(),
		"providers": providermetrics.ReadSnapshot(),
		"latency":   reqmetrics.ReadSnapshot(),
	}
}

type electionCounters interface{ Counters() leader.Counters }

type eventBusStats struct {
	Dropped uint64 `json:"dropped_total"`
}

func (a *App) liveMetrics() observeHandler.LiveMetrics {
	m := liveMetricsSnapshot()
	m["db_pool"] = database.ReadPoolStats(a.pool)
	m["redis_pool"] = redis.ReadPoolStats(a.redisClient)
	var counters leader.Counters
	if e, ok := a.election.(electionCounters); ok {
		counters = e.Counters()
	}
	m["leader"] = counters
	var bus eventBusStats
	if a.eventBus != nil {
		bus.Dropped = a.eventBus.Dropped()
	}
	m["event_bus"] = bus
	return m
}

func (a *App) evalMeterRunner() evalmeter.Runner {
	return wrapEvalRunner(a.buildEvalRunner())
}

func wrapEvalRunner(run EvalRunner) evalmeter.Runner {
	if run == nil {
		return nil
	}
	return func(ctx context.Context) (evalmeter.Result, error) {
		res, err := run(ctx)
		if err != nil {
			return evalmeter.Result{}, err
		}
		return meterResult(res), nil
	}
}

func meterResult(res EvalResult) evalmeter.Result {
	queries := make([]evalmeter.QueryResult, len(res.Queries))
	for i, q := range res.Queries {
		queries[i] = evalmeter.QueryResult{
			Query:    q.Query,
			Expect:   q.Expect,
			Passed:   q.Passed,
			Position: q.Position,
		}
	}
	return evalmeter.Result{
		Score:     res.Score,
		Baseline:  res.Baseline,
		Regressed: res.Regressed,
		Errored:   res.Errored,
		Queries:   queries,
	}
}

func (a *App) observeHealthProbe(ctx context.Context) observeHandler.DependencyHealth {
	h := a.dependencyHealth(ctx)
	return observeHandler.DependencyHealth{
		DB:    observeHandler.DepStatus(h.DB),
		Redis: observeHandler.DepStatus(h.Redis),
		Auth:  observeHandler.DepStatus(h.Auth),
		Detail: observeHandler.DependencyDetail{
			DBLatencyMs:    h.Detail.DBLatencyMs,
			DBError:        h.Detail.DBError,
			RedisLatencyMs: h.Detail.RedisLatencyMs,
			RedisError:     h.Detail.RedisError,
			AuthLatencyMs:  h.Detail.AuthLatencyMs,
			AuthError:      h.Detail.AuthError,
			CheckedAt:      h.Detail.CheckedAt,
		},
	}
}
