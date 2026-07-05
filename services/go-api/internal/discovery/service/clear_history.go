package service

import (
	"context"
	"fmt"

	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
)

type ClearSearchHistoryService struct {
	historyRepo ports.SearchHistoryRepository
}

func NewClearSearchHistoryService(historyRepo ports.SearchHistoryRepository) *ClearSearchHistoryService {
	return &ClearSearchHistoryService{historyRepo: historyRepo}
}

func (s *ClearSearchHistoryService) Execute(ctx context.Context, userId shared.UserId) error {
	if s.historyRepo == nil {
		return nil
	}
	if err := s.historyRepo.DeleteAllForUser(ctx, userId); err != nil {
		return fmt.Errorf("clear search history: %w", err)
	}
	return nil
}
