package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/httputil"

	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"

	feedbackHandler "altune/go-api/internal/feedback/adapters/handler"

	playbackHandler "altune/go-api/internal/playback/adapters/handler"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

func (a *App) mountRoutes(
	verifier auth.TokenVerifier,
	cat catalogWiring,
	queueHandler *playbackHandler.QueueHandler,
	discoveryH *discoveryHandler.DiscoveryHandler,
	feedbackH *feedbackHandler.FeedbackHandler,
) *chi.Mux {
	r := chi.NewRouter()

	r.Use(httputil.CorrelationID)
	r.Use(httputil.Recoverer)
	r.Use(httputil.RequestLogger)
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

	r.Get("/health", a.handleHealth)

	r.Route("/v1", func(r chi.Router) {
		r.Use(auth.Middleware(verifier))

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
		if feedbackH != nil {
			r.Mount("/feedback", feedbackH.Routes())
		}
		r.Handle("/events", newSSEHandler(a.eventBus))
	})

	return r
}
