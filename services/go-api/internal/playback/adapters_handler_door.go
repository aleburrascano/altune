package playback

import handler "altune/go-api/internal/playback/internal/adapters/handler"

type (
	QueueHandler   = handler.QueueHandler
	QueueSaveLimit = handler.QueueSaveLimit
)

var (
	NewQueueHandler                = handler.NewQueueHandler
	WithQueueStateClock            = handler.WithQueueStateClock
	WithQueueSaveLimit             = handler.WithQueueSaveLimit
	WithQueueStateRateLimitMetrics = handler.WithQueueStateRateLimitMetrics
)

var DefaultQueueStateRateLimit = handler.DefaultQueueStateRateLimit
