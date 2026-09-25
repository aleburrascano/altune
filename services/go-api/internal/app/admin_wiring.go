package app

import (
	"altune/go-api/internal/admin/evalmeter"
	"altune/go-api/internal/admin/eventtap"
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/leader"
	"altune/go-api/internal/shared/redis"
	"altune/go-api/internal/shared/reqmetrics"
	"context"
	"errors"
	"net/http"
	"time"

	adminHandler "altune/go-api/internal/admin/handler"

	authmetrics "altune/go-api/internal/auth/adapters/metrics"
	catalogmetrics "altune/go-api/internal/catalog/adapters/metrics"
	providermetrics "altune/go-api/internal/discovery/adapters/providermetrics"
	feedbackmetrics "altune/go-api/internal/feedback/adapters/metrics"
	playbackmetrics "altune/go-api/internal/playback/adapters/metrics"

	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"

	discoveryService "altune/go-api/internal/discovery/service"

	"github.com/go-chi/chi/v5"
)

// inspectorBudget caps the total wall time of one inspector replay. Each of the
// three replays a provider fan-out whose only other bound is each provider's
// own 10-15s HTTP client times up to 3 retries, which compounds into a
// multi-minute stuck admin request — holding one of the scarce in-flight
// inspector slots for all of it (#1996).
const inspectorBudget = 30 * time.Second

func (a *App) wireAdmin(
	ctx context.Context,
	cf clientFactory,
	r *chi.Mux,
	verifier auth.TokenVerifier,
	tap *eventtap.Tap,
	requestStore *requeststore.Store,
	searchSvc *discoveryService.Service,
	artistSvc *discoveryService.GetArtistContentService,
) {
	a.eventFeed = eventtap.NewFeed()
	a.eventFeed.Start(ctx, tap)
	var acqReader adminHandler.AcquisitionController
	if a.scheduler != nil {
		acqReader = a.scheduler
	}

	a.evalMeter = evalmeter.New(a.cfg.EvalMeterEnabled, 0, a.adminEvalRunner()).
		WithLeadership(a.leaderContext)
	a.whenLeader(jobEvalMeter, a.evalMeter.Start)
	adminH := adminHandler.New(a.adminHealthProbe, a.logRing).
		WithSupabaseLogin(a.cfg.SupabaseProjectURL, a.cfg.SupabaseAnonKey).
		WithShutdown(a.lifecycleDone).
		WithEventFeed(a.eventFeed).
		WithProviderHealth(a.providerHealth).
		WithAcquisition(acqReader).
		WithEvalMeter(a.evalMeter).
		WithAlertMonitor(a.alertMonitor).
		WithJobs(adminJobs{app: a}).
		WithRequestStore(requestStore).
		WithLiveMetrics(a.liveMetrics).
		WithMetricsHistory(discoveryPersistence.NewPgxMetricsRollup(a.pool)).
		WithDiscographyQuality(discoveryPersistence.NewPgxEventStore(a.pool))
	withAdminInspectors(adminH, a.cfg, cf.roundTripper(), searchSvc, artistSvc, inspectorBudget)
	mountAdmin(r, verifier, adminPrincipals{operator: a.cfg.OperatorUserID, readOnly: a.cfg.OperatorReadOnlyUserID}, adminH)
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

// withAdminInspectors registers reRun, inspectSearch and reRunDetail under one
// wall-time budget. They are one seam: three sibling admin search-debug
// features that replay the same discovery pipeline for the admin UI. They are
// wired here as the ReRunner, SearchInspector and DetailReRunner func types and
// otherwise share no prefix, so this registration block is their index — touch
// them together.
//
// reRun and inspectSearch take the context they are given, so the budget is
// applied here; reRunDetail takes it as an argument and spends it across its
// own sequential fan-out.
func withAdminInspectors(
	h *adminHandler.AdminHandler,
	cfg *config.Config,
	transport http.RoundTripper,
	searchSvc *discoveryService.Service,
	artistSvc *discoveryService.GetArtistContentService,
	budget time.Duration,
) *adminHandler.AdminHandler {
	return h.
		WithReRunner(func(ctx context.Context, query string, kinds []string) (requeststore.ReRunResult, error) {
			ctx, cancel := context.WithTimeout(ctx, budget)
			defer cancel()
			res, err := reRun(ctx, cfg, transport, searchSvc.BehavioralScoresSnapshot, query, kinds)
			return res, adminInspectorError(err)
		}).
		WithSearchInspector(func(ctx context.Context, query string, kinds []string) ([]requeststore.ResultRow, error) {
			ctx, cancel := context.WithTimeout(ctx, budget)
			defer cancel()
			rows, err := inspectSearch(ctx, searchSvc, query, kinds)
			return rows, adminInspectorError(err)
		}).
		WithDetailReRunner(func(ctx context.Context, query string) (requeststore.DetailReRunResult, error) {
			res, err := reRunDetail(ctx, searchSvc, artistSvc, budget, query)
			return res, adminInspectorError(err)
		})
}

// adminInspectorError translates the app-level failure classes of the admin
// inspectors into the admin handler's sentinels, so the HTTP boundary answers a
// caller's bad input with 400 and a total provider outage with its own 502
// instead of one untyped 502 for both. Unclassified errors pass through.
func adminInspectorError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errInvalidInspectorInput):
		return classifiedError{class: adminHandler.ErrInspectorInvalidInput, cause: err}
	case errors.Is(err, discoveryService.ErrAllProvidersFailed):
		return classifiedError{class: adminHandler.ErrInspectorProvidersDown, cause: err}
	default:
		return err
	}
}

// adminPrincipals names the two Supabase user ids the admin tree admits: the
// operator, and the optional read-only observer that may only GET. They travel
// together because the gate compares an incoming subject against both.
type adminPrincipals struct {
	operator string
	readOnly string
}

// mountAdmin mounts the /admin tree: the public index and login config, and the
// data routes behind bearer auth and the two-principal admin gate.
func mountAdmin(r chi.Router, verifier auth.TokenVerifier, principals adminPrincipals, adminH *adminHandler.AdminHandler) {
	r.Route("/admin", func(ar chi.Router) {
		ar.Use(adminHandler.NoStoreAndNosniff)
		ar.Get("/", adminH.ServeIndex)
		ar.Get("/config", adminH.ServeConfig)
		ar.Group(func(gr chi.Router) {
			gr.Use(authMiddleware(verifier))
			gr.Use(adminHandler.OperatorOrReadOnly(principals.operator, principals.readOnly))
			adminH.RegisterData(gr)
		})
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
