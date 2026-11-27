package httpserver

import (
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"net/http"
	"slices"
	"time"
)

type Outcome struct {
	Method   string
	Path     string
	Route    string
	Status   int
	Bytes    int
	Duration time.Duration
	UserID   string
}

type Observer func(r *http.Request) (*http.Request, func(Outcome))

const requestObserverPanic = "request.observer_panic"

func ObserveRequests(now clock.Func, observers ...Observer) func(http.Handler) http.Handler {
	if now == nil {
		panic("httpserver.ObserveRequests: a clock is required")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := now()
			sw := newStatusWriter(w)
			ctx, scope := withRequestScope(r.Context())
			r = r.WithContext(logging.ContextWithRequestFields(ctx, func() logging.RequestFields {
				return logging.RequestFields{UserID: scope.user(), Route: matchedRoute(r)}
			}))
			finishers := make([]func(Outcome), 0, len(observers))
			for _, observe := range observers {
				var finish func(Outcome)
				r, finish = observe(r)
				if finish != nil {
					finishers = append(finishers, finish)
				}
			}
			defer func(r *http.Request) {
				outcome := Outcome{
					Method:   r.Method,
					Path:     r.URL.Path,
					Route:    routePattern(r),
					Status:   sw.status,
					Bytes:    sw.bytes,
					Duration: now().Sub(start),
					UserID:   scope.user(),
				}
				all := slices.Concat(finishers, scope.routeFinishers())
				for i := len(all) - 1; i >= 0; i-- {
					finishGuarded(r.Context(), all[i], outcome)
				}
			}(r)
			next.ServeHTTP(sw, r)
		})
	}
}

func ObserveRoutes(observers ...Observer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := r.Context().Value(requestScopeKey{}).(*requestScope)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			for _, observe := range observers {
				var finish func(Outcome)
				r, finish = observe(r)
				if finish != nil {
					scope.addFinisher(finish)
				}
			}
			scope.setInner(r)
			next.ServeHTTP(w, r)
		})
	}
}

func finishGuarded(ctx context.Context, finish func(Outcome), outcome Outcome) {
	defer background.RecoverContext(ctx, requestObserverPanic)
	finish(outcome)
}

func LogRequest(r *http.Request) (*http.Request, func(Outcome)) {
	return r, func(o Outcome) {
		attrs := []any{
			logging.KeyMethod, o.Method,
			logging.KeyPath, o.Path,
			logging.KeyRoute, o.Route,
			logging.KeyStatus, o.Status,
			logging.KeyDurationMS, o.Duration.Milliseconds(),
			logging.KeyBytes, o.Bytes,
		}
		slog.InfoContext(innermostContext(r.Context()), "request.complete", attrs...)
	}
}
