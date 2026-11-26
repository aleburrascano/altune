package history

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/shared/ids"
	"context"
)

type ListSearchHistoryService struct {
	historyRepo ports.HistoryReader
}

func NewListSearchHistoryService(historyRepo ports.HistoryReader) (*ListSearchHistoryService, error) {
	if historyRepo == nil {
		return nil, ErrCollaboratorRequired
	}
	return &ListSearchHistoryService{historyRepo: historyRepo}, nil
}

func (s *ListSearchHistoryService) Execute(ctx context.Context, userId ids.UserId, limit int) ([]*domain.SearchHistoryEntry, error) {
	if limit <= 0 {
		limit = 10
	}
	return s.historyRepo.ListDistinctRecent(ctx, userId, limit)
}
