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

type QueueService struct {
	repo ports.QueueStateRepository
}

func NewQueueService(repo ports.QueueStateRepository) *QueueService {
	return &QueueService{repo: repo}
}

func (s *QueueService) Save(ctx context.Context, userId shared.UserId, input SaveQueueStateInput) error {
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

// Resume returns the user's saved snapshot, or the empty snapshot when none is
// stored — callers never receive nil.
func (s *QueueService) Resume(ctx context.Context, userId shared.UserId) (*domain.QueueState, error) {
	state, err := s.repo.GetForUser(ctx, userId)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return domain.EmptyQueueState(userId), nil
	}
	return state, nil
}
