package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/observe/evalmeter"
	"altune/go-api/internal/observe/eventtap"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/leader"
	"altune/go-api/internal/shared/redis"
	"altune/go-api/internal/shared/reqmetrics"
	"context"

	adminHandler "altune/go-api/internal/admin/handler"

	authmetrics "altune/go-api/internal/auth/adapters/metrics"
	catalogmetrics "altune/go-api/internal/catalog/adapters/metrics"
	providermetrics "altune/go-api/internal/discovery/adapters/providermetrics"
	feedbackmetrics "altune/go-api/internal/feedback/adapters/metrics"
	playbackmetrics "altune/go-api/internal/playback/adapters/metrics"

	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"

	"github.com/go-chi/chi/v5"
)

func (a *App) wireAdmin(ctx context.Context, r *chi.Mux, verifier auth.TokenVerifier, tap *eventtap.Tap) {
	a.eventFeed = eventtap.NewFeed()
	a.eventFeed.Start(ctx, tap)
	a.evalMeter = evalmeter.New(a.cfg.EvalMeterEnabled, 0, a.adminEvalRunner()).
		WithLeadership(a.leaderContext)
	a.whenLeader(jobEvalMeter, a.evalMeter.Start)
	mountAdmin(r, verifier, adminPrincipals{operator: a.cfg.OperatorUserID, readOnly: a.cfg.OperatorReadOnlyUserID}, a.buildAdminHandler())
}

func (a *App) buildAdminHandler() *adminHandler.AdminHandler {
	var acqReader adminHandler.AcquisitionController
	if a.scheduler != nil {
		acqReader = a.scheduler
	}
	return adminHandler.New(a.adminHealthProbe, a.logRing).
		WithShutdown(a.lifecycleDone).
		WithEventFeed(a.eventFeed).
		WithAcquisition(acqReader).
		WithEvalMeter(a.evalMeter).
		WithAlertMonitor(a.alertMonitor).
		WithJobs(adminJobs{app: a}).
		WithLiveMetrics(a.liveMetrics).
		WithMetricsHistory(discoveryPersistence.NewPgxMetricsRollup(a.pool)).
		WithDiscographyQuality(discoveryPersistence.NewPgxEventStore(a.pool))
}

func liveMetricsSnapshot() adminHandler.LiveMetrics {
	return adminHandler.LiveMetrics{
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

func (a *App) liveMetrics() adminHandler.LiveMetrics {
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

// adminPrincipals names the two Supabase user ids the admin tree admits: the
// operator, and the optional read-only observer that may only GET. They travel
// together because the gate compares an incoming subject against both.
type adminPrincipals struct {
	operator string
	readOnly string
}

func mountAdmin(r chi.Router, verifier auth.TokenVerifier, principals adminPrincipals, adminH *adminHandler.AdminHandler) {
	r.Route("/admin", func(ar chi.Router) {
		ar.Use(adminHandler.NoStoreAndNosniff)
		ar.Use(authMiddleware(verifier))
		ar.Use(adminHandler.OperatorOrReadOnly(principals.operator, principals.readOnly))
		adminH.RegisterData(ar)
	})
}

// adminJobs adapts the job registry's kill switch and health signal into
// the admin handler's JobSwitchboard at the wiring boundary, keeping
// jobs.go free of any dependency on admin/handler.
type adminJobs struct{ app *App }

func (j adminJobs) Jobs() []adminHandler.JobStatus {
	health := j.app.JobHealth()
	out := make([]adminHandler.JobStatus, len(health))
	for i, h := range health {
		out[i] = adminHandler.JobStatus(h)
	}
	return out
}

func (j adminJobs) SetJobEnabled(name string, enabled bool) (adminHandler.JobStatus, bool) {
	h, ok := j.app.SetJobEnabled(jobName(name), enabled)
	return adminHandler.JobStatus(h), ok
}

// adminEvalRunner adapts the app-owned EvalRunner into admin/evalmeter's
// Runner at the wiring boundary, mapping the app-owned result to the meter's
// wire DTO so eval_runner.go no longer depends on admin/evalmeter. A nil app
// runner (meter disabled) stays nil so the meter treats it as unset.
func (a *App) adminEvalRunner() evalmeter.Runner {
	run := a.buildEvalRunner()
	if run == nil {
		return nil
	}
	return func(ctx context.Context) (evalmeter.Result, error) {
		res, err := run(ctx)
		if err != nil {
			return evalmeter.Result{}, err
		}
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
		}, nil
	}
}

// adminHealthProbe adapts the app-owned DependencyHealth into the admin
// handler's presentation DTO at the wiring boundary, keeping health.go free of
// any dependency on admin/handler.
func (a *App) adminHealthProbe(ctx context.Context) adminHandler.DependencyHealth {
	h := a.dependencyHealth(ctx)
	return adminHandler.DependencyHealth{
		DB:    adminHandler.DepStatus(h.DB),
		Redis: adminHandler.DepStatus(h.Redis),
		Auth:  adminHandler.DepStatus(h.Auth),
		Detail: adminHandler.DependencyDetail{
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
