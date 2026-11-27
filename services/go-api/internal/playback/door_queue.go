package playback

import (
	domain "altune/go-api/internal/playback/internal/core/domain"
	ports "altune/go-api/internal/playback/internal/core/ports"
	queue "altune/go-api/internal/playback/internal/service/queue"
)

type (
	SaveQueueStateInput    = queue.SaveQueueStateInput
	SaveQueuePositionInput = queue.SaveQueuePositionInput
	QueueSource            = domain.QueueSource
	RepeatMode             = domain.RepeatMode
	NowPlayingTrack        = ports.NowPlayingTrack
	OwnerScanPage          = ports.OwnerScanPage
)

const RepeatOne = domain.RepeatOne
