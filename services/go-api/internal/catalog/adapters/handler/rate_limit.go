package handler

import (
	shareddomain "altune/go-api/internal/shared/domain"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/requestuser"
	"net/http"
	"time"
)

var (
	streamRateLimit        = httputil.RateLimit{Every: time.Second, Burst: 120}
	audioURLRateLimit      = httputil.RateLimit{Every: 250 * time.Millisecond, Burst: 300}
	trackWriteRateLimit    = httputil.RateLimit{Every: 500 * time.Millisecond, Burst: 200}
	playlistWriteRateLimit = httputil.RateLimit{Every: time.Second, Burst: 60}
)

var (
	errAudioRateLimited = shareddomain.NewError(shareddomain.CodeCatalogAudioRateLimited, "too many audio requests, try again later")
	errWriteRateLimited = shareddomain.NewError(shareddomain.CodeCatalogWriteRateLimited, "too many writes, try again later")
)

type rateLimitMiddleware = func(http.Handler) http.Handler

func newAudioRateLimiter(limit httputil.RateLimit, now func() time.Time) rateLimitMiddleware {
	return newUserRateLimiter(limit, now, errAudioRateLimited)
}

func newWriteRateLimiter(limit httputil.RateLimit, now func() time.Time) rateLimitMiddleware {
	return newUserRateLimiter(limit, now, errWriteRateLimited)
}

func newUserRateLimiter(limit httputil.RateLimit, now func() time.Time, refused error) rateLimitMiddleware {
	return httputil.NewUserRateLimiter(limit, now).Middleware(httputil.UserKey(requestuser.UserIDFromContext), refused, nil)
}
