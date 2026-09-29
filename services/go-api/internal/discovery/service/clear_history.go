package service

import (
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
	"time"
)

const ClearSearchHistoryAction = "clear_search_history"

type ClearSearchHistoryService struct {
	historyRepo ports.HistoryEraser
}

func NewClearSearchHistoryService(historyRepo ports.HistoryEraser) *ClearSearchHistoryService {
	return &ClearSearchHistoryService{historyRepo: historyRepo}
}

func (s *ClearSearchHistoryService) Execute(ctx context.Context, userId shared.UserId) error {
	if s.historyRepo == nil {
		return nil
	}
	if err := s.historyRepo.EraseSearchTextForUser(ctx, userId); err != nil {
		return fmt.Errorf("clear search history: %w", err)
	}
	auditHistoryCleared(ctx, userId)
	return nil
}

func auditHistoryCleared(ctx context.Context, userId shared.UserId) {
	slog.InfoContext(ctx, "discovery.search_history_cleared",
		slog.String("action", ClearSearchHistoryAction),
		slog.String("user_id", userId.String()),
		logging.CorrelationAttr(ctx),
		slog.Time("at", time.Now().UTC()),
	)
}
