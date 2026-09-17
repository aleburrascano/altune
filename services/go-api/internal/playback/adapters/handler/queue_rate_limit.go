package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared/httputil"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// QueueStateRateLimit bounds how often one user may hit /queue-state. Every
// PUT is a JSON decode, domain validation and a Postgres upsert on the shared
// pool, so a leaked token or a runaway client retry loop must not be able to
// drive unbounded write QPS.
//
// The limiter is per instance: behind N replicas a user can reach N times the
// budget. A shared (Redis-backed) limiter is a separate, later step.
type QueueStateRateLimit struct {
	// Every is the steady-state refill interval: one request per Every.
	Every time.Duration
	// Burst is how many requests an idle user may make back to back.
	Burst int
}

// DefaultQueueStateRateLimit sits far above legitimate use. The mobile client
// autosaves every 15s plus on every background/inactive transition, so even
// several signed-in devices stay well under one request per 2s sustained; the
// burst absorbs a resume GET plus a flurry of app-switch saves.
var DefaultQueueStateRateLimit = QueueStateRateLimit{
	Every: 2 * time.Second,
	Burst: 20,
}

// WithQueueStateRateLimitMetrics makes the limiter count every request it
// refuses through m. A nil m keeps the no-op default.
func WithQueueStateRateLimitMetrics(m ports.RateLimitMetrics) QueueHandlerOption {
	return func(c *queueHandlerConfig) {
		if m != nil {
			c.rateLimitMetrics = m
		}
	}
}

// queueRateLimitedError routes the throttle through httputil.HandleServiceError
// so the body carries the same {detail, code} envelope as every other error.
type queueRateLimitedError struct{}

func (queueRateLimitedError) Error() string     { return "too many queue state requests, try again later" }
func (queueRateLimitedError) HTTPStatus() int   { return http.StatusTooManyRequests }
func (queueRateLimitedError) ErrorCode() string { return "playback.rate_limited" }

// userRateLimiter is a token bucket per user id. Memory is bounded by the users
// active within one refill window: a bucket idle long enough to have refilled
// completely is indistinguishable from a fresh one, so it is dropped.
//
// Buckets are held in two generations rather than one map so that reclaiming
// costs a swap instead of a scan. Every request takes mu, so a scan there would
// stall every other user's /queue-state for a pause growing with the instance's
// active-user count (#1568). The two maps are disjoint: active holds the
// buckets touched since the last rotation, cooling the ones that survived it.
type userRateLimiter struct {
	mu        sync.Mutex
	limit     QueueStateRateLimit
	now       func() time.Time
	metrics   ports.RateLimitMetrics
	active    map[string]*rate.Limiter
	cooling   map[string]*rate.Limiter
	rotatedAt time.Time
}

func newUserRateLimiter(limit QueueStateRateLimit, now func() time.Time, metrics ports.RateLimitMetrics) *userRateLimiter {
	if limit.Burst < 1 {
		limit.Burst = 1
	}
	return &userRateLimiter{
		limit:   limit,
		now:     now,
		metrics: metrics,
		active:  make(map[string]*rate.Limiter),
		cooling: make(map[string]*rate.Limiter),
	}
}

// idleTTL is how long a bucket takes to refill from empty to full.
func (l *userRateLimiter) idleTTL() time.Duration {
	return l.limit.Every * time.Duration(l.limit.Burst)
}

// allow spends one token for key. When the bucket is empty it reports false
// and how long until the next token, without spending anything.
func (l *userRateLimiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.rotate(now)
	limiter := l.bucket(key)
	if limiter.AllowN(now, 1) {
		return true, 0
	}
	res := limiter.ReserveN(now, 1)
	defer res.CancelAt(now)
	return false, res.DelayFrom(now)
}

// bucket returns key's limiter, creating it on first sight, and moves it into
// the generation the next rotation keeps.
func (l *userRateLimiter) bucket(key string) *rate.Limiter {
	if limiter, ok := l.active[key]; ok {
		return limiter
	}
	limiter, survived := l.cooling[key]
	if !survived {
		limiter = rate.NewLimiter(rate.Every(l.limit.Every), l.limit.Burst)
	}
	delete(l.cooling, key)
	l.active[key] = limiter
	return limiter
}

// rotate drops the generation no request has touched for a full idle TTL, so a
// bucket is reclaimed between one and two idle TTLs after its last request and
// never while it can still be holding spent tokens. Freeing the dropped map is
// the collector's work, off mu.
func (l *userRateLimiter) rotate(now time.Time) {
	if now.Sub(l.rotatedAt) < l.idleTTL() {
		return
	}
	l.rotatedAt = now
	l.cooling = l.active
	l.active = make(map[string]*rate.Limiter)
}

// middleware throttles authenticated callers per user id. It must run after
// auth; an unauthenticated request is passed through so the handler's own
// RequireUserID answers it with 401.
//
// Every refusal is counted (#1566): a 429 leaves no log line of its own, so the
// counter is the only signal that a client is stuck in a retry loop. It is one
// number for the instance, not one per user, so a flood of distinct principals
// cannot grow what the counter costs.
func (l *userRateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userId, ok := auth.UserIDFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		allowed, wait := l.allow(userId.String())
		if !allowed {
			l.metrics.QueueStateRateLimited()
			w.Header().Set("Retry-After", retryAfterSeconds(wait))
			httputil.HandleServiceError(w, r, queueRateLimitedError{})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// retryAfterSeconds renders a wait as whole seconds, rounded up and never 0 so
// a client honouring it cannot spin.
func retryAfterSeconds(wait time.Duration) string {
	return strconv.Itoa(max(1, int(math.Ceil(wait.Seconds()))))
}
