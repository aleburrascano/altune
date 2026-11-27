package handler

import (
	ports "altune/go-api/internal/playback/internal"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/requestuser"
	"net/http"
	"time"
)

type QueueSaveLimit = httpserver.RateLimit

const (
	queueStateRateInterval = 2 * time.Second
	queueStateRateBurst    = 20
)

func DefaultQueueSaveLimit() QueueSaveLimit {
	return QueueSaveLimit{Every: queueStateRateInterval, Burst: queueStateRateBurst}
}

func newUserRateLimiter(limit QueueSaveLimit, now func() time.Time, metrics ports.RateLimitMetrics) func(http.Handler) http.Handler {
	return requestuser.LimitPerUser(limit, now, errQueueRateLimited, metrics.QueueStateRateLimited)
}
