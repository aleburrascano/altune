package service

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
	"time"
)

const ClearSearchHistoryAction = "clear_search_history"

type ClearSearchHistoryService struct {
	historyRepo ports.HistoryEraser
	now         clock.Func
}

func NewClearSearchHistoryService(historyRepo ports.HistoryEraser, now clock.Func) (*ClearSearchHistoryService, error) {
	if historyRepo == nil {
		return nil, domain.ErrHistoryCollaboratorRequired
	}
	if now == nil {
		return nil, domain.ErrHistoryClockRequired
	}
	return &ClearSearchHistoryService{historyRepo: historyRepo, now: now}, nil
}

func MustNewClearSearchHistoryService(historyRepo ports.HistoryEraser, now clock.Func) *ClearSearchHistoryService {
	s, err := NewClearSearchHistoryService(historyRepo, now)
	if err != nil {
		panic(err)
	}
	return s
}

func (s *ClearSearchHistoryService) Execute(ctx context.Context, userId ids.UserId) error {
	if err := s.historyRepo.EraseSearchTextForUser(ctx, userId); err != nil {
		return fmt.Errorf("clear search history: %w", err)
	}
	auditHistoryCleared(ctx, userId, s.now())
	return nil
}

func auditHistoryCleared(ctx context.Context, userId ids.UserId, at time.Time) {
	slog.InfoContext(ctx, "discovery.search_history_cleared",
		slog.String("action", ClearSearchHistoryAction),
		logging.CorrelationAttr(ctx),
		slog.Time("at", at.UTC()),
	)
}
