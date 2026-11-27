package handler

import (
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/requestuser"
	"net/http"
	"time"
)

const (
	audioURLRateInterval   = 250 * time.Millisecond
	trackWriteRateInterval = 500 * time.Millisecond

	streamRateBurst        = 120
	audioURLRateBurst      = 300
	trackWriteRateBurst    = 200
	playlistWriteRateBurst = 60
)

var (
	streamRateLimit        = httpserver.RateLimit{Every: time.Second, Burst: streamRateBurst}
	audioURLRateLimit      = httpserver.RateLimit{Every: audioURLRateInterval, Burst: audioURLRateBurst}
	trackWriteRateLimit    = httpserver.RateLimit{Every: trackWriteRateInterval, Burst: trackWriteRateBurst}
	playlistWriteRateLimit = httpserver.RateLimit{Every: time.Second, Burst: playlistWriteRateBurst}
)

var (
	errAudioRateLimited = apierror.NewError(apierror.CodeCatalogAudioRateLimited, "too many audio requests, try again later")
	errWriteRateLimited = apierror.NewError(apierror.CodeCatalogWriteRateLimited, "too many writes, try again later")
)

type rateLimitMiddleware = func(http.Handler) http.Handler

func newAudioRateLimiter(limit httpserver.RateLimit, now clock.Func) rateLimitMiddleware {
	return requestuser.LimitPerUser(limit, now, errAudioRateLimited, nil)
}

func newWriteRateLimiter(limit httpserver.RateLimit, now clock.Func) rateLimitMiddleware {
	return requestuser.LimitPerUser(limit, now, errWriteRateLimited, nil)
}
