package playback

import domain "altune/go-api/internal/playback/domain"

type (
	QueuePosition   = domain.QueuePosition
	QueueState      = domain.QueueState
	QueueStateInput = domain.QueueStateInput
)

var NewQueueState = domain.NewQueueState

const RepeatOff = domain.RepeatOff
