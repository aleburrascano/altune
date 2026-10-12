package handler

import (
	"altune/go-api/internal/catalog/adapters/authbridge"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared/httputil"
	"net/http"
	"time"
)

type AudioRateLimit = httputil.RateLimit

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

type rateLimitMiddleware = func(http.Handler) http.Handler

func newAudioRateLimiter(limit AudioRateLimit, now func() time.Time) rateLimitMiddleware {
	return newUserRateLimiter(limit, now, errAudioRateLimited)
}

func newWriteRateLimiter(limit AudioRateLimit, now func() time.Time) rateLimitMiddleware {
	return newUserRateLimiter(limit, now, errWriteRateLimited)
}

func newUserRateLimiter(limit AudioRateLimit, now func() time.Time, refused error) rateLimitMiddleware {
	return httputil.NewUserRateLimiter(limit, now).Middleware(requestUserKey, refused, nil)
}

func requestUserKey(r *http.Request) (string, bool) {
	userId, ok := authbridge.UserIDFromContext(r.Context())
	return userId.String(), ok
}
