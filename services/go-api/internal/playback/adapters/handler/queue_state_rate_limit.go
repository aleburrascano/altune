package handler

import (
	"altune/go-api/internal/playback/adapters/authbridge"
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared/httputil"
	"net/http"
	"time"
)

type QueueStateRateLimit = httputil.RateLimit

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
	return httputil.NewUserRateLimiter(limit, now).Middleware(requestUserKey, domain.QueueRateLimitedError{}, metrics.QueueStateRateLimited)
}

func requestUserKey(r *http.Request) (string, bool) {
	userId, ok := authbridge.UserIDFromContext(r.Context())
	return userId.String(), ok
}
