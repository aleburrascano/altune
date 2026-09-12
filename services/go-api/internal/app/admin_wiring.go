package app

import (
	"altune/go-api/internal/admin/evalmeter"
	"altune/go-api/internal/admin/eventtap"
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/auth"
	"context"

	adminHandler "altune/go-api/internal/admin/handler"

	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"

	discoveryService "altune/go-api/internal/discovery/service"

	"github.com/go-chi/chi/v5"
)

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
			return reRunDetail(ctx, searchSvc, artistSvc, query)
		}).
		WithMetricsHistory(discoveryPersistence.NewPgxMetricsRollup(a.pool))
	r.Route("/admin", func(ar chi.Router) {
		ar.Get("/", adminH.ServeIndex)
		ar.Get("/config", adminH.ServeConfig)
		ar.Group(func(gr chi.Router) {
			gr.Use(auth.Middleware(verifier))
			gr.Use(adminHandler.OperatorOnly(a.cfg.OperatorUserID))
			adminH.RegisterData(gr)
		})
	})
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
		DB:    h.DB,
		Redis: h.Redis,
		Auth:  h.Auth,
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
