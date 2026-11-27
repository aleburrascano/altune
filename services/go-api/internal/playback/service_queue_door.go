package playback

import queue "altune/go-api/internal/playback/internal/service/queue"

type QueueService = queue.Service

var (
	ErrQueueClockRequired = queue.ErrClockRequired
	MustNewQueueService   = queue.MustNew
	NewQueueService       = queue.New
)
