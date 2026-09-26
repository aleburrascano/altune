package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/reqmetrics"
	"time"

	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"

	feedbackHandler "altune/go-api/internal/feedback/adapters/handler"

	playbackHandler "altune/go-api/internal/playback/adapters/handler"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

const apiWriteTimeout = 60 * time.Second

func (a *App) mountRoutes(
	verifier auth.TokenVerifier,
	cat catalogWiring,
	queueHandler *playbackHandler.QueueHandler,
	discoveryH *discoveryHandler.DiscoveryHandler,
	feedbackH *feedbackHandler.FeedbackHandler,
) *chi.Mux {
	r := a.newRouter(apiWriteTimeout)

	r.Get("/health", a.handleHealth)

	r.Route("/v1", func(r chi.Router) {
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
		mountFeedback(r, feedbackH)
		r.Handle("/events", newSSEHandler(a.eventBus, a.cfg.SSEMaxConns).withShutdown(a.lifecycleDone))
	})

	return r
}

func (a *App) newRouter(writeTimeout time.Duration) *chi.Mux {
	r := chi.NewRouter()

	r.Use(httputil.CorrelationID)
	r.Use(latencyMiddleware(reqmetrics.Observe))
	r.Use(httputil.WriteDeadline(writeTimeout))
	r.Use(httputil.RequestLogger)
	r.Use(httputil.Recoverer)
	r.Use(httputil.MaxBodySize(1 << 20))
	corsHeaders := []string{"Accept", "Authorization", "Content-Type"}
	if a.cfg.IsDevelopment() {
		corsHeaders = append(corsHeaders, "ngrok-skip-browser-warning")
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   a.cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   corsHeaders,
		ExposedHeaders:   []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	return r
}
