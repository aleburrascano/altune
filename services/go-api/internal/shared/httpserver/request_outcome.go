package httpserver

import (
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"net/http"
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
			r = r.WithContext(ctx)
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
				for i := len(finishers) - 1; i >= 0; i-- {
					finishGuarded(r.Context(), finishers[i], outcome)
				}
			}(r)
			next.ServeHTTP(sw, r)
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
			logging.KeyDuration, o.Duration,
			logging.KeyBytes, o.Bytes,
		}
		if o.UserID != "" {
			attrs = append(attrs, logging.KeyUserID, o.UserID)
		}
		slog.InfoContext(r.Context(), "request.complete", attrs...)
	}
}
