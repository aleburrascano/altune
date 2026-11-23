package router

import (
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/reqmetrics"
	"altune/go-api/internal/shared/telemetry"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

const maxBodyBytes = 1 << 20

type Settings struct {
	WriteTimeout   time.Duration
	RequestTimeout time.Duration
	CORSOrigins    []string
	Development    bool
	Latency        *reqmetrics.Registry
}

func New(s Settings) *chi.Mux {
	r := chi.NewRouter()

	r.Use(httputil.CorrelationID)
	r.Use(telemetry.Middleware(untraced))
	r.Use(reqmetrics.Middleware(s.Latency.Observe))
	r.Use(httputil.WriteDeadline(s.WriteTimeout))
	r.Use(v1RequestTimeout(s.RequestTimeout))
	r.Use(httputil.RequestLogger)
	r.Use(httputil.Recoverer)
	r.Use(httputil.MaxBodySize(maxBodyBytes))
	r.Use(corsHandler(s))
	return r
}

func corsHandler(s Settings) func(http.Handler) http.Handler {
	headers := []string{"Accept", "Authorization", "Content-Type", "Idempotency-Key", telemetry.AppVersionHeader, telemetry.PlatformHeader}
	if s.Development {
		headers = append(headers, "ngrok-skip-browser-warning")
	}
	return cors.Handler(cors.Options{
		AllowedOrigins:   s.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   headers,
		ExposedHeaders:   []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           300,
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

func untraced(r *http.Request) bool {
	return r.URL.Path == "/health" || isLongLivedRoute(r)
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
