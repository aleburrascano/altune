package service

import (
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/shared/ids"
	"context"
	"fmt"
)

type SaveQueuePositionInput struct {
	CurrentIdx     int
	CurrentTrackId string
	PositionMs     int64
}

func (s *QueueService) SavePosition(ctx context.Context, userId ids.UserId, input SaveQueuePositionInput) error {
	position, err := domain.NewQueuePosition(domain.QueuePositionInput{
		UserId:         userId,
		CurrentIdx:     input.CurrentIdx,
		CurrentTrackId: input.CurrentTrackId,
		PositionMs:     input.PositionMs,
	}, s.now())
	if err != nil {
		return fmt.Errorf("invalid queue position: %w", err)
	}
	return s.repo.UpdatePosition(ctx, position)
}
