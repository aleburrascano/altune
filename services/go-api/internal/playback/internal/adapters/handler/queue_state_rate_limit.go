package handler

import (
	"altune/go-api/internal/playback/internal/core/ports"
	shareddomain "altune/go-api/internal/shared/domain"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/requestuser"
	"net/http"
	"time"
)

type QueueStateRateLimit = httputil.RateLimit

var errQueueRateLimited = shareddomain.NewError(shareddomain.CodePlaybackRateLimited, "too many queue state requests, try again later")

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

func newUserRateLimiter(limit QueueStateRateLimit, now func() time.Time, metrics ports.RateLimitMetrics) func(http.Handler) http.Handler {
	return httputil.NewUserRateLimiter(limit, now).Middleware(httputil.UserKey(requestuser.UserIDFromContext), errQueueRateLimited, metrics.QueueStateRateLimited)
}
