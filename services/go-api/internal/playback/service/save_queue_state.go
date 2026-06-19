package service

import (
	"context"
	"time"

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
	state := &domain.QueueState{
		UserId:     userId,
		TrackIds:   input.TrackIds,
		CurrentIdx: input.CurrentIdx,
		PositionMs: input.PositionMs,
		Shuffled:   input.Shuffled,
		RepeatMode: input.RepeatMode,
		SourceId:   input.SourceId,
		UpdatedAt:  time.Now(),
	}
	return s.repo.Upsert(ctx, state)
}
