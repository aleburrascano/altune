package app

import (
	"altune/go-api/internal/app/health"
	"altune/go-api/internal/app/router"
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/platform/eventstream"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/clock"
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
) *chi.Mux {
	r := a.newRouter(apiWriteTimeout)
	discoveryRoutes := discovery.Routes()

	requireAuth := authMiddleware(verifier, a.metrics)

	r.Method(http.MethodGet, "/health", health.NewEndpoint(a.dependencyHealth, buildCommit, clock.Now))

	router.MountV1(r, apiRequestTimeout, router.V1{
		Bounded: func(r chi.Router) {
			discovery.PublicRoutes(r)

			r.Group(func(r chi.Router) {
				r.Use(requireAuth)

				cat.libraryRoutes.Bounded(r)
				if cat.acquisitionOn {
					r.Post("/tracks/{trackId}/reacquire", cat.reacquireH.HandleReacquire)
					r.Post("/tracks/{trackId}/retry", cat.retryH.HandleRetryAcquisition)
				}
				playbackRoutes.Bounded(r)
				discoveryRoutes.Bounded(r)
				r.Mount("/telemetry", clientEventRoutes)
			})
		},
		LongLived: func(r chi.Router) {
			r.Use(requireAuth)

			cat.libraryRoutes.LongLived(r)
			playbackRoutes.LongLived(r)
			discoveryRoutes.LongLived(r)
			r.Handle("/events", eventstream.NewEventStreamHandler(clock.Now, clock.NewTicker, a.eventBus, a.cfg.SSEMaxConns).WithShutdown(a.lifecycleDone))
		},
	})

	return r
}

func (a *application) newRouter(writeTimeout time.Duration) *chi.Mux {
	return router.New(router.Settings{
		WriteTimeout: writeTimeout,
		CORSOrigins:  a.cfg.CORSOrigins,
		Development:  a.cfg.IsDevelopment(),
		Latency:      a.latency,
	})
}
