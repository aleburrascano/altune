package handler

import (
	"altune/go-api/internal/playback/internal/core/ports"
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/requestuser"
	"net/http"
	"time"
)

type QueueSaveLimit = httpserver.RateLimit

var errQueueRateLimited = apierror.NewError(apierror.CodePlaybackRateLimited, "too many queue state requests, try again later")

var DefaultQueueStateRateLimit = QueueSaveLimit{
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

func newUserRateLimiter(limit QueueSaveLimit, now func() time.Time, metrics ports.RateLimitMetrics) func(http.Handler) http.Handler {
	return httpserver.NewUserRateLimiter(limit, now).Middleware(httpserver.UserKey(requestuser.UserIDFromContext), errQueueRateLimited, metrics.QueueStateRateLimited)
}
