package playback

import queue "altune/go-api/internal/playback/internal/core/queue"

type QueueService = queue.Service

var NewQueueService = queue.New
