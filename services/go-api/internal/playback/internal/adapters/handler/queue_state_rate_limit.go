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

const queueStateRateInterval = 2 * time.Second

var DefaultQueueStateRateLimit = QueueSaveLimit{
	Every: queueStateRateInterval,
	Burst: 20,
}

func newUserRateLimiter(limit QueueSaveLimit, now func() time.Time, metrics ports.RateLimitMetrics) func(http.Handler) http.Handler {
	return requestuser.LimitPerUser(limit, now, errQueueRateLimited, metrics.QueueStateRateLimited)
}
