package domain

import (
	"altune/go-api/internal/shared"
	"fmt"
	"time"
)

type QueuePosition struct {
	UserId         shared.UserId
	CurrentIdx     int
	CurrentTrackId string
	PositionMs     int64
	UpdatedAt      time.Time
}

type QueuePositionInput struct {
	UserId         shared.UserId
	CurrentIdx     int
	CurrentTrackId string
	PositionMs     int64
}

func NewQueuePosition(in QueuePositionInput) (*QueuePosition, error) {
	p := &QueuePosition{
		UserId:         in.UserId,
		CurrentIdx:     in.CurrentIdx,
		CurrentTrackId: in.CurrentTrackId,
		PositionMs:     in.PositionMs,
		UpdatedAt:      handledNow(),
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *QueuePosition) Validate() error {
	if p.PositionMs < 0 {
		return newValidationError(codePositionMsNegative, fmt.Sprintf("positionMs must be non-negative, got %d", p.PositionMs))
	}
	if !indexInBounds(p.CurrentIdx, MaxQueueLength) {
		return newValidationError(codeCurrentIdxOutOfRange, fmt.Sprintf("currentIdx %d out of range [0, %d)", p.CurrentIdx, MaxQueueLength))
	}
	if p.CurrentTrackId == "" {
		return newValidationError(codeCurrentTrackIdMissing, "currentTrackId is required")
	}
	return stringStorable("currentTrackId", p.CurrentTrackId)
}
