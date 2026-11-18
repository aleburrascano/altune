package playback

import (
	domain "altune/go-api/internal/playback/domain"
	ports "altune/go-api/internal/playback/internal/core/ports"
	queue "altune/go-api/internal/playback/internal/core/queue"
)

type (
	SaveQueueStateInput    = queue.SaveQueueStateInput
	SaveQueuePositionInput = queue.SaveQueuePositionInput
	QueueServiceOption     = queue.Option
	QueueSource            = domain.QueueSource
	RepeatMode             = domain.RepeatMode
	NowPlayingTrack        = ports.NowPlayingTrack
	OwnerScanPage          = ports.OwnerScanPage
)

const (
	RepeatAll = domain.RepeatAll
	RepeatOne = domain.RepeatOne
)

var (
	ErrQueuePositionMismatch = domain.ErrQueuePositionMismatch
	ErrCorruptStoredState    = ports.ErrCorruptStoredState
)
