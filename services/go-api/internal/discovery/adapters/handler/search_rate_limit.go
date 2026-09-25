package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/httputil"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RequestLimit admits at most Max requests per user in any rolling Window.
type RequestLimit struct {
	Max    int
	Window time.Duration
}

// DiscoveryRateLimits sizes the per-user throttles on the discovery routes
// whose cost is highest per call.
type DiscoveryRateLimits struct {
	Search  RequestLimit
	Suggest RequestLimit
	Events  RequestLimit
	// Content is one budget shared by every provider fan-out route rather than
	// one budget each, because what they spend is shared too: MusicBrainz
	// allows one request a second across all callers together, so nine
	// separate budgets would let one account hold nine times the share.
	Content   RequestLimit
	Favorites RequestLimit
}

// DefaultDiscoveryRateLimits is sized to each route's fan-out. One /search
// call can reach up to 14 provider requests plus vocabulary-index writes, so
// 60 a minute (the client's 300 ms search-as-you-type debounce plus paging
// stays well under it) caps an account near 840 outbound calls a minute.
// /suggest is a local vocabulary lookup fired while typing, so it gets a wider
// budget. A content call reaches one or two providers, so 180 a minute caps an
// account near search's own outbound share, and still leaves room for a client
// opening detail screens (about seven calls each) as fast as a person can
// scroll. /events is one size-capped insert with no fan-out, and the mobile
// outbox can drain a 50-entry backlog in a single pass, so it gets 300.
var DefaultDiscoveryRateLimits = DiscoveryRateLimits{
	Search:    RequestLimit{Max: 60, Window: time.Minute},
	Suggest:   RequestLimit{Max: 120, Window: time.Minute},
	Events:    RequestLimit{Max: 300, Window: time.Minute},
	Content:   RequestLimit{Max: 180, Window: time.Minute},
	Favorites: RequestLimit{Max: 60, Window: time.Minute},
}

// rateLimitedError routes the throttle through the typed
// httputil.HandleServiceError contract so clients see a stable code.
type rateLimitedError struct{}

func (rateLimitedError) Error() string     { return "too many requests, try again later" }
func (rateLimitedError) HTTPStatus() int   { return http.StatusTooManyRequests }
func (rateLimitedError) ErrorCode() string { return "discovery.rate_limited" }

// userRateLimiter is a sliding-window log per user. Memory is bounded: each
// log holds at most limit.Max timestamps, and logs idle for a full window are
// swept at most once per window so the hot path never scans every user.
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

// admit records one request for key, or reports how long until the oldest
// logged request leaves the window without recording anything.
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

// untilFree is how long until the oldest logged request leaves the window; a
// non-positive Max logs nothing and so rejects for a whole window.
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

// inWindow drops the chronologically ordered timestamps that fell out of the
// window ending at now.
func inWindow(times []time.Time, now time.Time, window time.Duration) []time.Time {
	for i, t := range times {
		if now.Sub(t) < window {
			return times[i:]
		}
	}
	return nil
}

// middleware throttles authenticated callers. Unauthenticated requests pass
// through untouched so the handler's own 401 stays the response.
func (l *userRateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := auth.UserIDFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		if wait, admitted := l.admit(userID.String()); !admitted {
			w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(wait)))
			httputil.HandleServiceError(w, r, rateLimitedError{})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// retryAfterSeconds rounds up so a client that honors the header is admitted.
func retryAfterSeconds(wait time.Duration) int {
	secs := int((wait + time.Second - 1) / time.Second)
	return max(secs, 1)
}
