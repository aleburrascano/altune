package handler

import (
	"altune/go-api/internal/playback/internal/core/ports"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/requestuser"
	"net/http"
	"time"
)

type QueueStateRateLimit = httputil.RateLimit

type queueRateLimitedError struct{}

func (queueRateLimitedError) Error() string     { return "too many queue state requests, try again later" }
func (queueRateLimitedError) HTTPStatus() int   { return http.StatusTooManyRequests }
func (queueRateLimitedError) ErrorCode() string { return "playback.rate_limited" }

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
	return httputil.NewUserRateLimiter(limit, now).Middleware(httputil.UserKey(requestuser.UserIDFromContext), queueRateLimitedError{}, metrics.QueueStateRateLimited)
}
