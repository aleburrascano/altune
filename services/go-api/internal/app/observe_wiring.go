package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/config"
	"context"

	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"
	observeHandler "altune/go-api/internal/observe/handler"

	"github.com/go-chi/chi/v5"
)

func (a *App) wireObserve(r *chi.Mux, verifier auth.TokenVerifier) {
	deps := observeHandler.Deps{
		Health:      a.observeHealthProbe,
		Logs:        a.logRing,
		Events:      a.eventFeed,
		Eval:        a.evalMeter,
		Acquisition: a.observeAcquisition(),
		LiveMetrics: a.observeLiveMetrics,
		Discography: discoveryPersistence.NewPgxEventStore(a.pool),
		Shutdown:    a.lifecycleDone,
	}
	mountObserve(r, verifier, observePrincipal(a.cfg), observeHandler.New(deps))
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

func (a *App) observeLiveMetrics() observeHandler.LiveMetrics {
	return observeHandler.LiveMetrics(a.liveMetrics())
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
