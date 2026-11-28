package app

import (
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/platform/eventstream"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/requestmetrics"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

const maxBodyBytes = 1 << 20

type routerSettings struct {
	WriteTimeout time.Duration
	CORSOrigins  []string
	Development  bool
	Latency      *requestmetrics.Registry
}

func buildRouter(s routerSettings) *chi.Mux {
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

func corsHandler(s routerSettings) func(http.Handler) http.Handler {
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

func v1ResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

type v1Routes struct {
	Bounded   func(chi.Router)
	LongLived func(chi.Router)
}

func mountV1(r chi.Router, requestTimeout time.Duration, v1 v1Routes) {
	r.Route("/v1", func(r chi.Router) {
		r.Use(v1ResponseHeaders)
		r.Group(func(r chi.Router) {
			r.Use(httpserver.RequestTimeout(requestTimeout), httpserver.ObserveRoutes(telemetry.RequestSpan(spanEveryRequest)))
			v1.Bounded(r)
		})
		r.Group(v1.LongLived)
	})
}

func spanEveryRequest(*http.Request) bool { return false }

const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = 120 * time.Second
)

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
	}
}

var errServerStopped = errors.New("http server goroutine ended before shutdown")

func serve(ctx context.Context, httpServer *http.Server) error {
	errCh := make(chan error, 1)
	listenDone := background.Go(ctx, "app.server_panic", func() {
		slog.InfoContext(ctx, "server listening", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	})

	select {
	case err := <-errCh:
		return err
	case <-listenDone:
		select {
		case err := <-errCh:
			return err
		default:
			return errServerStopped
		}
	case <-ctx.Done():
		slog.InfoContext(ctx, "shutting down")
	}

	drainCtx, endDrain := background.DetachCancelable(ctx)
	defer endDrain()
	drainServer(drainCtx, httpServer, serverDrainTimeout)
	return nil
}

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

	r.Method(http.MethodGet, "/health", monitoring.NewHealthEndpoint(a.dependencyHealth, buildCommit, clock.Now))
	mountV1(r, apiRequestTimeout, v1Routes{
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
	return buildRouter(routerSettings{
		WriteTimeout: writeTimeout,
		CORSOrigins:  a.cfg.CORSOrigins,
		Development:  a.cfg.IsDevelopment(),
		Latency:      a.latency,
	})
}
