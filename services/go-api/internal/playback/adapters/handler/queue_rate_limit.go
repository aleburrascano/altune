package handler

import (
	"altune/go-api/internal/auth"
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

// queueRateLimitedError routes the throttle through httputil.HandleServiceError
// so the body carries the same {detail, code} envelope as every other error.
type queueRateLimitedError struct{}

func (queueRateLimitedError) Error() string     { return "too many queue state requests, try again later" }
func (queueRateLimitedError) HTTPStatus() int   { return http.StatusTooManyRequests }
func (queueRateLimitedError) ErrorCode() string { return "playback.rate_limited" }

type userBucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// userRateLimiter is a token bucket per user id. Memory is bounded by the users
// active within one refill window: a bucket idle long enough to have refilled
// completely is indistinguishable from a fresh one, so it is evicted.
type userRateLimiter struct {
	mu        sync.Mutex
	limit     QueueStateRateLimit
	now       func() time.Time
	buckets   map[string]*userBucket
	lastSweep time.Time
}

func newUserRateLimiter(limit QueueStateRateLimit, now func() time.Time) *userRateLimiter {
	if limit.Burst < 1 {
		limit.Burst = 1
	}
	return &userRateLimiter{limit: limit, now: now, buckets: make(map[string]*userBucket)}
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
	l.sweep(now)
	limiter := l.bucket(key, now)
	if limiter.AllowN(now, 1) {
		return true, 0
	}
	res := limiter.ReserveN(now, 1)
	defer res.CancelAt(now)
	return false, res.DelayFrom(now)
}

// bucket returns key's limiter, creating it on first sight, and marks it seen.
func (l *userRateLimiter) bucket(key string, now time.Time) *rate.Limiter {
	b, ok := l.buckets[key]
	if !ok {
		b = &userBucket{limiter: rate.NewLimiter(rate.Every(l.limit.Every), l.limit.Burst)}
		l.buckets[key] = b
	}
	b.lastSeen = now
	return b.limiter
}

// sweep drops fully refilled buckets, at most once per idle TTL so the scan
// cost stays amortised across requests.
func (l *userRateLimiter) sweep(now time.Time) {
	ttl := l.idleTTL()
	if now.Sub(l.lastSweep) < ttl {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if now.Sub(b.lastSeen) >= ttl {
			delete(l.buckets, k)
		}
	}
}

// middleware throttles authenticated callers per user id. It must run after
// auth; an unauthenticated request is passed through so the handler's own
// RequireUserID answers it with 401.
func (l *userRateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userId, ok := auth.UserIDFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		allowed, wait := l.allow(userId.String())
		if !allowed {
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
