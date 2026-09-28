package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared/httputil"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type AudioRateLimit struct {
	Every time.Duration
	Burst int
}

var DefaultStreamRateLimit = AudioRateLimit{
	Every: time.Second,
	Burst: 120,
}

var DefaultAudioURLRateLimit = AudioRateLimit{
	Every: 250 * time.Millisecond,
	Burst: 300,
}

var DefaultTrackWriteRateLimit = AudioRateLimit{
	Every: 500 * time.Millisecond,
	Burst: 200,
}

var DefaultPlaylistWriteRateLimit = AudioRateLimit{
	Every: time.Second,
	Burst: 60,
}

var (
	errAudioRateLimited = &domain.CodedError{
		Msg:    "too many audio requests, try again later",
		Status: http.StatusTooManyRequests,
		Code:   "catalog.audio_rate_limited",
	}
	errWriteRateLimited = &domain.CodedError{
		Msg:    "too many writes, try again later",
		Status: http.StatusTooManyRequests,
		Code:   "catalog.write_rate_limited",
	}
)

type audioUserBucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type audioRateLimiter struct {
	mu        sync.Mutex
	limit     AudioRateLimit
	refused   error
	now       func() time.Time
	buckets   map[string]*audioUserBucket
	lastSweep time.Time
}

func newAudioRateLimiter(limit AudioRateLimit, now func() time.Time) *audioRateLimiter {
	return newUserRateLimiter(limit, now, errAudioRateLimited)
}

func newWriteRateLimiter(limit AudioRateLimit, now func() time.Time) *audioRateLimiter {
	return newUserRateLimiter(limit, now, errWriteRateLimited)
}

func newUserRateLimiter(limit AudioRateLimit, now func() time.Time, refused error) *audioRateLimiter {
	if limit.Burst < 1 {
		limit.Burst = 1
	}
	if now == nil {
		now = time.Now
	}
	return &audioRateLimiter{
		limit:   limit,
		refused: refused,
		now:     now,
		buckets: make(map[string]*audioUserBucket),
	}
}

func (l *audioRateLimiter) idleTTL() time.Duration {
	return l.limit.Every * time.Duration(l.limit.Burst)
}

func (l *audioRateLimiter) allow(key string) (bool, time.Duration) {
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

func (l *audioRateLimiter) bucket(key string, now time.Time) *rate.Limiter {
	b, ok := l.buckets[key]
	if !ok {
		b = &audioUserBucket{limiter: rate.NewLimiter(rate.Every(l.limit.Every), l.limit.Burst)}
		l.buckets[key] = b
	}
	b.lastSeen = now
	return b.limiter
}

func (l *audioRateLimiter) sweep(now time.Time) {
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

func (l *audioRateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userId, ok := auth.UserIDFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		allowed, wait := l.allow(userId.String())
		if !allowed {
			w.Header().Set("Retry-After", audioRetryAfterSeconds(wait))
			httputil.HandleServiceError(w, r, l.refused)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func audioRetryAfterSeconds(wait time.Duration) string {
	return strconv.Itoa(max(1, int(math.Ceil(wait.Seconds()))))
}
