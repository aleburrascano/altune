package app

import (
	"altune/go-api/internal/app/health"
	"altune/go-api/internal/app/router"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/eventstream"
	"altune/go-api/internal/playback"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	apiWriteTimeout   = 60 * time.Second
	apiRequestTimeout = 50 * time.Second
)

func (a *App) mountRoutes(
	verifier auth.TokenVerifier,
	cat catalogWiring,
	queueHandler *playback.QueueHandler,
	discoveryH *discovery.DiscoveryHandler,
) *chi.Mux {
	r := a.newRouter(apiWriteTimeout)

	r.Method(http.MethodGet, "/health", health.NewEndpoint(a.dependencyHealth, buildCommit, time.Now))

	r.Route("/v1", func(r chi.Router) {
		r.Use(router.V1ResponseHeaders)
		discoveryH.PublicRoutes(r, discovery.DefaultAuthFailureLimit)

		r.Group(func(r chi.Router) {
			r.Use(authMiddleware(verifier))

			r.Route("/tracks", func(r chi.Router) {
				r.Mount("/", cat.trackHandler.Routes())
			})
			cat.streamHandler.Routes(r)
			cat.audioURLHandler.Routes(r)
			if cat.reacquireH != nil {
				r.Post("/tracks/{trackId}/reacquire", cat.reacquireH.HandleReacquire)
			}
			if cat.retryH != nil {
				r.Post("/tracks/{trackId}/retry", cat.retryH.HandleRetryAcquisition)
			}
			r.Mount("/library", cat.libraryHandler.Routes())
			r.Mount("/playlists", cat.playlistHandler.Routes())
			r.Mount("/playback", queueHandler.Routes())
			r.Mount("/discovery", discoveryH.Routes())
			r.Handle("/events", eventstream.NewEventStreamHandler(a.eventBus, a.cfg.SSEMaxConns).WithShutdown(a.lifecycleDone))
		})
	})

	return r
}

func (a *App) newRouter(writeTimeout time.Duration) *chi.Mux {
	return router.New(router.Settings{
		WriteTimeout:   writeTimeout,
		RequestTimeout: apiRequestTimeout,
		CORSOrigins:    a.cfg.CORSOrigins,
		Development:    a.cfg.IsDevelopment(),
		Latency:        a.latency,
	})
}
