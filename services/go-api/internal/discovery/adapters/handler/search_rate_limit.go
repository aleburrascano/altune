package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/httputil"
	"net/http"
	"sync"
	"time"
)

type RequestLimit struct {
	Max    int
	Window time.Duration
}

type DiscoveryRateLimits struct {
	Search    RequestLimit
	Suggest   RequestLimit
	Events    RequestLimit
	Content   RequestLimit
	Favorites RequestLimit
	History   RequestLimit
}

var DefaultDiscoveryRateLimits = DiscoveryRateLimits{
	Search:    RequestLimit{Max: 60, Window: time.Minute},
	Suggest:   RequestLimit{Max: 120, Window: time.Minute},
	Events:    RequestLimit{Max: 300, Window: time.Minute},
	Content:   RequestLimit{Max: 180, Window: time.Minute},
	Favorites: RequestLimit{Max: 60, Window: time.Minute},
	History:   RequestLimit{Max: 60, Window: time.Minute},
}

type rateLimitedError struct{}

func (rateLimitedError) Error() string     { return "too many requests, try again later" }
func (rateLimitedError) HTTPStatus() int   { return http.StatusTooManyRequests }
func (rateLimitedError) ErrorCode() string { return "discovery.rate_limited" }

type userRateLimiter struct {
	mu        sync.Mutex
	limit     RequestLimit
	now       func() time.Time
	logs      map[string][]time.Time
	lastSweep time.Time
}

func newUserRateLimiter(limit RequestLimit, now func() time.Time) *userRateLimiter {
	return &userRateLimiter{limit: limit, now: now, logs: make(map[string][]time.Time), lastSweep: now()}
}

func (l *userRateLimiter) admit(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweep(now)
	times := inWindow(l.logs[key], now, l.limit.Window)
	if len(times) >= l.limit.Max {
		l.logs[key] = times
		return l.untilFree(times, now), false
	}
	l.logs[key] = append(times, now)
	return 0, true
}

func (l *userRateLimiter) untilFree(times []time.Time, now time.Time) time.Duration {
	if len(times) == 0 {
		return l.limit.Window
	}
	return times[0].Add(l.limit.Window).Sub(now)
}

func (l *userRateLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < l.limit.Window {
		return
	}
	l.lastSweep = now
	for k, times := range l.logs {
		if len(inWindow(times, now, l.limit.Window)) == 0 {
			delete(l.logs, k)
		}
	}
}

func inWindow(times []time.Time, now time.Time, window time.Duration) []time.Time {
	for i, t := range times {
		if now.Sub(t) < window {
			return times[i:]
		}
	}
	return nil
}

func (l *userRateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := auth.UserIDFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		if wait, admitted := l.admit(userID.String()); !admitted {
			w.Header().Set("Retry-After", httputil.RetryAfterSeconds(wait))
			httputil.HandleServiceError(w, r, rateLimitedError{})
			return
		}
		next.ServeHTTP(w, r)
	})
}
