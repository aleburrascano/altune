package handler

import (
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/requestuser"
	"net/http"
	"time"
)

var (
	streamRateLimit        = httpserver.RateLimit{Every: time.Second, Burst: 120}
	audioURLRateLimit      = httpserver.RateLimit{Every: 250 * time.Millisecond, Burst: 300}
	trackWriteRateLimit    = httpserver.RateLimit{Every: 500 * time.Millisecond, Burst: 200}
	playlistWriteRateLimit = httpserver.RateLimit{Every: time.Second, Burst: 60}
)

var (
	errAudioRateLimited = apierror.NewError(apierror.CodeCatalogAudioRateLimited, "too many audio requests, try again later")
	errWriteRateLimited = apierror.NewError(apierror.CodeCatalogWriteRateLimited, "too many writes, try again later")
)

type rateLimitMiddleware = func(http.Handler) http.Handler

func newAudioRateLimiter(limit httpserver.RateLimit, now func() time.Time) rateLimitMiddleware {
	return newUserRateLimiter(limit, now, errAudioRateLimited)
}

func newWriteRateLimiter(limit httpserver.RateLimit, now func() time.Time) rateLimitMiddleware {
	return newUserRateLimiter(limit, now, errWriteRateLimited)
}

func newUserRateLimiter(limit httpserver.RateLimit, now func() time.Time, refused error) rateLimitMiddleware {
	return httpserver.NewUserRateLimiter(limit, now).Middleware(httpserver.UserKey(requestuser.UserIDFromContext), refused, nil)
}
