package service

import (
	"context"

	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
)

type GetQueueStateService struct {
	repo ports.QueueStateRepository
}

func NewGetQueueStateService(repo ports.QueueStateRepository) *GetQueueStateService {
	return &GetQueueStateService{repo: repo}
}

func (s *GetQueueStateService) Execute(
	ctx context.Context,
	userId shared.UserId,
) (*domain.QueueState, error) {
	return s.repo.GetForUser(ctx, userId)
}
