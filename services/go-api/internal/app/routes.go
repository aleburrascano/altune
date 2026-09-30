package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/reqmetrics"
	"net/http"
	"strings"
	"time"

	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"

	feedbackHandler "altune/go-api/internal/feedback/adapters/handler"

	playbackHandler "altune/go-api/internal/playback/adapters/handler"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

const (
	apiWriteTimeout   = 60 * time.Second
	apiRequestTimeout = 50 * time.Second
)

func (a *App) requestBudget() time.Duration {
	if a.requestTimeout > 0 {
		return a.requestTimeout
	}
	return apiRequestTimeout
}

func isLongLivedRoute(r *http.Request) bool {
	path := r.URL.Path
	return path == "/v1/events" ||
		(r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/tracks/") && strings.HasSuffix(path, "/audio"))
}

func v1RequestTimeout(d time.Duration) func(http.Handler) http.Handler {
	bounded := httputil.RequestTimeout(d)
	return func(next http.Handler) http.Handler {
		withDeadline := bounded(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/v1/") || isLongLivedRoute(r) {
				next.ServeHTTP(w, r)
				return
			}
			withDeadline.ServeHTTP(w, r)
		})
	}
}

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
		discoveryH.PublicRoutes(r, discoveryHandler.DefaultAuthFailureLimit)

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
			mountFeedback(r, feedbackH)
			r.Handle("/events", newSSEHandler(a.eventBus, a.cfg.SSEMaxConns).withShutdown(a.lifecycleDone))
		})
	})

	return r
}

func (a *App) newRouter(writeTimeout time.Duration) *chi.Mux {
	r := chi.NewRouter()

	r.Use(httputil.CorrelationID)
	r.Use(latencyMiddleware(reqmetrics.Observe))
	r.Use(httputil.WriteDeadline(writeTimeout))
	r.Use(v1RequestTimeout(a.requestBudget()))
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
