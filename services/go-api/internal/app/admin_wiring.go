package app

import (
	"altune/go-api/internal/admin/evalmeter"
	"altune/go-api/internal/admin/eventtap"
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/auth"
	"context"
	"time"

	adminHandler "altune/go-api/internal/admin/handler"

	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"

	discoveryService "altune/go-api/internal/discovery/service"

	"github.com/go-chi/chi/v5"
)

// detailReRunBudget caps the total wall time of the /rerun-detail sequential
// provider fan-out. Without it, six back-to-back no-timeout provider calls
// (each bounded only by its own 10-15s HTTP client, up to 3 retries) can
// compound into a multi-minute stuck admin request.
const detailReRunBudget = 30 * time.Second

func (a *App) wireAdmin(
	ctx context.Context,
	r *chi.Mux,
	verifier auth.TokenVerifier,
	tap *eventtap.Tap,
	requestStore *requeststore.Store,
	searchSvc *discoveryService.Service,
	artistSvc *discoveryService.GetArtistContentService,
) {
	a.eventFeed = eventtap.NewFeed()
	a.eventFeed.Start(ctx, tap)
	var acqReader adminHandler.AcquisitionStatusReader
	if a.scheduler != nil {
		acqReader = a.scheduler
	}

	a.evalMeter = evalmeter.New(a.cfg.EvalMeterEnabled, 0, a.adminEvalRunner())
	a.whenLeader("eval meter", a.evalMeter.Start)
	adminH := adminHandler.New(a.adminHealthProbe, a.logRing).
		WithSupabaseLogin(a.cfg.SupabaseProjectURL, a.cfg.SupabaseAnonKey).
		WithEventFeed(a.eventFeed).
		WithProviderHealth(a.providerHealth).
		WithAcquisition(acqReader).
		WithEvalMeter(a.evalMeter).
		WithAlertMonitor(a.alertMonitor).
		WithJobs(adminJobs{app: a}).
		WithRequestStore(requestStore).
		// reRun, inspectSearch and reRunDetail are one seam: three sibling
		// admin search-debug features that replay the same discovery pipeline
		// for the admin UI. They are wired here as the ReRunner, SearchInspector
		// and DetailReRunner func types and otherwise share no prefix, so this
		// registration block is their index — touch them together.
		WithReRunner(func(ctx context.Context, query string, kinds []string) (requeststore.ReRunResult, error) {
			return reRun(ctx, a.cfg, defaultLiveTransport, searchSvc.BehavioralScoresSnapshot, query, kinds)
		}).
		WithSearchInspector(func(ctx context.Context, query string, kinds []string) ([]requeststore.ResultRow, error) {
			return inspectSearch(ctx, searchSvc, query, kinds)
		}).
		WithDetailReRunner(func(ctx context.Context, query string) (requeststore.DetailReRunResult, error) {
			return reRunDetail(ctx, searchSvc, artistSvc, detailReRunBudget, query)
		}).
		WithMetricsHistory(discoveryPersistence.NewPgxMetricsRollup(a.pool))
	mountAdmin(r, verifier, a.cfg.OperatorUserID, adminH)
}

// mountAdmin mounts the /admin tree: the public index and login config, and the
// data routes behind bearer auth and the operator gate.
func mountAdmin(r chi.Router, verifier auth.TokenVerifier, operatorUserID string, adminH *adminHandler.AdminHandler) {
	r.Route("/admin", func(ar chi.Router) {
		ar.Get("/", adminH.ServeIndex)
		ar.Get("/config", adminH.ServeConfig)
		ar.Group(func(gr chi.Router) {
			gr.Use(auth.Middleware(verifier))
			gr.Use(adminHandler.OperatorOnly(operatorUserID))
			adminH.RegisterData(gr)
		})
	})
}

// adminJobs adapts the leader ticker's job kill switch and health signal into
// the admin handler's JobSwitchboard at the wiring boundary, keeping
// leader_ticker.go free of any dependency on admin/handler.
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
	h, ok := j.app.SetJobEnabled(name, enabled)
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
