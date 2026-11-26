package playback

import handler "altune/go-api/internal/playback/internal/adapters/handler"

type (
	QueueHandler   = handler.QueueHandler
	QueueSaveLimit = handler.QueueSaveLimit
)

var (
	ErrQueueHandlerClockRequired        = handler.ErrClockRequired
	ErrQueueHandlerCollaboratorRequired = handler.ErrCollaboratorRequired
	MustNewQueueHandler                 = handler.MustNewQueueHandler
	NewQueueHandler                     = handler.NewQueueHandler
)

var DefaultQueueStateRateLimit = handler.DefaultQueueStateRateLimit
