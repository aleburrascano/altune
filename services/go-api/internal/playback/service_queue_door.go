package playback

import queue "altune/go-api/internal/playback/service/queue"

type QueueService = queue.Service

var (
	NewQueueService          = queue.New
	WithNowPlayingEnrichment = queue.WithNowPlayingEnrichment
)
