package httpserver

import (
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type RateLimit struct {
	Every time.Duration
	Burst int
}

type userKeyFunc func(*http.Request) (string, bool)

func UserKey(userID func(context.Context) (ids.UserId, bool)) userKeyFunc {
	return func(r *http.Request) (string, bool) {
		id, ok := userID(r.Context())
		return id.String(), ok
	}
}

type userRateLimiter struct {
	mu        sync.Mutex
	limit     RateLimit
	now       clock.Func
	active    map[string]*rate.Limiter
	cooling   map[string]*rate.Limiter
	rotatedAt time.Time
}

var errClockRequired = errors.New("httpserver: the rate limiter requires a clock")

func newUserRateLimiter(limit RateLimit, now clock.Func) (*userRateLimiter, error) {
	if now == nil {
		return nil, errClockRequired
	}
	if limit.Burst < 1 {
		limit.Burst = 1
	}
	return &userRateLimiter{
		limit:   limit,
		now:     now,
		active:  make(map[string]*rate.Limiter),
		cooling: make(map[string]*rate.Limiter),
	}, nil
}

func MustNewUserRateLimiter(limit RateLimit, now clock.Func) *userRateLimiter {
	l, err := newUserRateLimiter(limit, now)
	if err != nil {
		panic(err)
	}
	return l
}

func (l *userRateLimiter) idleTTL() time.Duration {
	return l.limit.Every * time.Duration(l.limit.Burst)
}

func (l *userRateLimiter) Allow(key string) (bool, time.Duration) {
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

func (l *userRateLimiter) Middleware(userKey userKeyFunc, refused error, onRefused func()) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, ok := userKey(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			if allowed, wait := l.Allow(key); !allowed {
				if onRefused != nil {
					onRefused()
				}
				w.Header().Set("Retry-After", RetryAfterSeconds(wait))
				HandleServiceError(w, r, refused)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
