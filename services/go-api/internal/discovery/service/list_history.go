package service

import (
	"context"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
)

type ListSearchHistoryService struct {
	historyRepo ports.SearchHistoryRepository
}

func NewListSearchHistoryService(historyRepo ports.SearchHistoryRepository) *ListSearchHistoryService {
	return &ListSearchHistoryService{historyRepo: historyRepo}
}

func (s *ListSearchHistoryService) Execute(ctx context.Context, userId shared.UserId, limit int) ([]*domain.SearchHistoryEntry, error) {
	if s.historyRepo == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	return s.historyRepo.ListDistinctRecent(ctx, userId, limit)
}
