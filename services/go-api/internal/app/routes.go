package app

import (
	"altune/go-api/internal/app/health"
	"altune/go-api/internal/app/router"
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/platform/eventstream"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/search"
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
	discoveryH *search.DiscoveryHandler,
) *chi.Mux {
	r := a.newRouter(apiWriteTimeout)

	r.Method(http.MethodGet, "/health", health.NewEndpoint(a.dependencyHealth, buildCommit, time.Now))

	r.Route("/v1", func(r chi.Router) {
		r.Use(router.V1ResponseHeaders)
		discoveryH.PublicRoutes(r, search.DefaultAuthFailureLimit)

		r.Group(func(r chi.Router) {
			r.Use(authMiddleware(verifier))

			r.Route("/tracks", func(r chi.Router) {
				r.Mount("/", cat.trackHandler.Routes())
			})
			cat.streamHandler.Routes(r)
			cat.audioURLHandler.Routes(r)
			cat.acquisitionRoutes(r)
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
