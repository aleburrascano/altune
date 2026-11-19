package playback

import handler "altune/go-api/internal/playback/internal/adapters/handler"

type (
	QueueHandler        = handler.QueueHandler
	QueueStateRateLimit = handler.QueueStateRateLimit
)

var (
	NewQueueHandler                = handler.NewQueueHandler
	WithQueueStateClock            = handler.WithQueueStateClock
	WithQueueStateRateLimit        = handler.WithQueueStateRateLimit
	WithQueueStateRateLimitMetrics = handler.WithQueueStateRateLimitMetrics
)

var DefaultQueueStateRateLimit = handler.DefaultQueueStateRateLimit
