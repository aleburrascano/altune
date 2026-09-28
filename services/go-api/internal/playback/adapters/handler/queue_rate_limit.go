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

type QueueStateRateLimit struct {
	Every time.Duration
	Burst int
}

var DefaultQueueStateRateLimit = QueueStateRateLimit{
	Every: 2 * time.Second,
	Burst: 20,
}

func WithQueueStateRateLimitMetrics(m ports.RateLimitMetrics) QueueHandlerOption {
	return func(c *queueHandlerConfig) {
		if m != nil {
			c.rateLimitMetrics = m
		}
	}
}

type queueRateLimitedError struct{}

func (queueRateLimitedError) Error() string     { return "too many queue state requests, try again later" }
func (queueRateLimitedError) HTTPStatus() int   { return http.StatusTooManyRequests }
func (queueRateLimitedError) ErrorCode() string { return "playback.rate_limited" }

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

func (l *userRateLimiter) idleTTL() time.Duration {
	return l.limit.Every * time.Duration(l.limit.Burst)
}

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

func (l *userRateLimiter) rotate(now time.Time) {
	if now.Sub(l.rotatedAt) < l.idleTTL() {
		return
	}
	l.rotatedAt = now
	l.cooling = l.active
	l.active = make(map[string]*rate.Limiter)
}

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

func retryAfterSeconds(wait time.Duration) string {
	return strconv.Itoa(max(1, int(math.Ceil(wait.Seconds()))))
}
