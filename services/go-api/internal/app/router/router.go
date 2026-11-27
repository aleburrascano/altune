package router

import (
	"altune/go-api/internal/platform/requestmetrics"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/telemetry"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

const maxBodyBytes = 1 << 20

type Settings struct {
	WriteTimeout time.Duration
	CORSOrigins  []string
	Development  bool
	Latency      *requestmetrics.Registry
}

func New(s Settings) *chi.Mux {
	r := chi.NewRouter()

	r.Use(httpserver.CorrelationID)
	r.Use(httpserver.ObserveRequests(clock.Now,
		requestmetrics.Observer(s.Latency.Observe),
		httpserver.LogRequest,
	))
	r.Use(httpserver.WriteDeadline(clock.Now, s.WriteTimeout))
	r.Use(httpserver.Recoverer)
	r.Use(httpserver.MaxBodySize(maxBodyBytes))
	r.Use(corsHandler(s))
	return r
}

const (
	lastEventIDHeader = "Last-Event-ID"
	retryAfterHeader  = "Retry-After"

	corsPreflightMaxAgeSeconds = 300
)

func corsHandler(s Settings) func(http.Handler) http.Handler {
	headers := []string{
		"Accept", "Authorization", "Content-Type", httpserver.IdempotencyKeyHeader,
		httpserver.CorrelationIDHeader, lastEventIDHeader,
		telemetry.AppVersionHeader, telemetry.PlatformHeader,
	}
	if s.Development {
		headers = append(headers, "ngrok-skip-browser-warning")
	}
	return cors.Handler(cors.Options{
		AllowedOrigins: s.CORSOrigins,
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: headers,
		ExposedHeaders: []string{"Content-Length", httpserver.CorrelationIDHeader, retryAfterHeader},
		MaxAge:         corsPreflightMaxAgeSeconds,
	})
}

func V1ResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

type V1 struct {
	Bounded   func(chi.Router)
	LongLived func(chi.Router)
}

func MountV1(r chi.Router, requestTimeout time.Duration, v1 V1) {
	r.Route("/v1", func(r chi.Router) {
		r.Use(V1ResponseHeaders)
		r.Group(func(r chi.Router) {
			r.Use(httpserver.RequestTimeout(requestTimeout), httpserver.ObserveRoutes(telemetry.RequestSpan(spanEveryRequest)))
			v1.Bounded(r)
		})
		r.Group(v1.LongLived)
	})
}

func spanEveryRequest(*http.Request) bool { return false }
