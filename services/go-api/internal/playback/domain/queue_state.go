package domain

import (
	"time"

	"altune/go-api/internal/shared"
)

type QueueState struct {
	UserId      shared.UserId
	TrackIds    []string
	CurrentIdx  int
	PositionMs  int64
	Shuffled    bool
	RepeatMode  string
	SourceId    string
	UpdatedAt   time.Time
}
