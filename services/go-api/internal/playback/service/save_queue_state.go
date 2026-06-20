package service

import (
	"context"
	"fmt"

	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
)

type SaveQueueStateInput struct {
	TrackIds   []string
	CurrentIdx int
	PositionMs int64
	Shuffled   bool
	RepeatMode string
	SourceId   string
}

type SaveQueueStateService struct {
	repo ports.QueueStateRepository
}

func NewSaveQueueStateService(repo ports.QueueStateRepository) *SaveQueueStateService {
	return &SaveQueueStateService{repo: repo}
}

func (s *SaveQueueStateService) Execute(
	ctx context.Context,
	userId shared.UserId,
	input SaveQueueStateInput,
) error {
	rm, err := domain.ParseRepeatMode(input.RepeatMode)
	if err != nil {
		return fmt.Errorf("invalid repeat mode: %w", err)
	}
	state, err := domain.NewQueueState(userId, input.TrackIds, input.CurrentIdx, input.PositionMs, input.Shuffled, rm, input.SourceId)
	if err != nil {
		return fmt.Errorf("invalid queue state: %w", err)
	}
	return s.repo.Upsert(ctx, state)
}
