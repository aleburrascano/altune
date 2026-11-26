package playback

import handler "altune/go-api/internal/playback/internal/adapters/handler"

type (
	QueueHandler   = handler.QueueHandler
	QueueSaveLimit = handler.QueueSaveLimit
)

var NewQueueHandler = handler.NewQueueHandler

var DefaultQueueStateRateLimit = handler.DefaultQueueStateRateLimit
