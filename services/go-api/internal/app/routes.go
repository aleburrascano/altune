package app

import (
	"altune/go-api/internal/app/health"
	"altune/go-api/internal/app/router"
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/platform/eventstream"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/clock"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	apiWriteTimeout   = 60 * time.Second
	apiRequestTimeout = 50 * time.Second
)

func (a *application) mountRoutes(
	verifier auth.TokenVerifier,
	cat catalogWiring,
	playbackRoutes playback.Routes,
	discovery *search.Module,
	clientEventRoutes http.Handler,
) (*chi.Mux, error) {
	r := a.newRouter(apiWriteTimeout)
	discoveryRoutes := discovery.Routes()
	events, err := eventstream.New(eventstream.Deps{
		Now:           clock.Now,
		NewTicker:     clock.NewTicker,
		Bus:           a.eventBus,
		MaxConnsTotal: a.cfg.SSEMaxConns,
		Shutdown:      a.lifecycleDone,
	})
	if err != nil {
		return nil, fmt.Errorf("event stream: %w", err)
	}
	downloadRoutes := cat.download.Routes()

	requireAuth := authMiddleware(verifier, a.metrics)

	r.Method(http.MethodGet, "/health", health.NewEndpoint(a.dependencyHealth, buildCommit, clock.Now))

	router.MountV1(r, apiRequestTimeout, router.V1{
		Bounded: func(r chi.Router) {
			discovery.PublicRoutes(r)

			r.Group(func(r chi.Router) {
				r.Use(requireAuth)

				cat.libraryRoutes.Bounded(r)
				downloadRoutes.Bounded(r)
				playbackRoutes.Bounded(r)
				discoveryRoutes.Bounded(r)
				r.Mount("/telemetry", clientEventRoutes)
			})
		},
		LongLived: func(r chi.Router) {
			r.Use(requireAuth)

			cat.libraryRoutes.LongLived(r)
			downloadRoutes.LongLived(r)
			playbackRoutes.LongLived(r)
			discoveryRoutes.LongLived(r)
			events.LongLived(r)
		},
	})

	return r, nil
}

func (a *application) newRouter(writeTimeout time.Duration) *chi.Mux {
	return router.New(router.Settings{
		WriteTimeout: writeTimeout,
		CORSOrigins:  a.cfg.CORSOrigins,
		Development:  a.cfg.IsDevelopment(),
		Latency:      a.latency,
	})
}
