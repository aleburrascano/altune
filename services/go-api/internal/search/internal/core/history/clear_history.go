package history

import (
	"altune/go-api/internal/search/internal/core/ports"
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

func NewClearSearchHistoryService(historyRepo ports.HistoryEraser, now clock.Func) *ClearSearchHistoryService {
	if now == nil {
		panic("history: NewClearSearchHistoryService requires a clock")
	}
	return &ClearSearchHistoryService{historyRepo: historyRepo, now: now}
}

func (s *ClearSearchHistoryService) Execute(ctx context.Context, userId ids.UserId) error {
	if s.historyRepo == nil {
		return nil
	}
	if err := s.historyRepo.EraseSearchTextForUser(ctx, userId); err != nil {
		return fmt.Errorf("clear search history: %w", err)
	}
	auditHistoryCleared(ctx, userId, s.now())
	return nil
}

func auditHistoryCleared(ctx context.Context, userId ids.UserId, at time.Time) {
	slog.InfoContext(ctx, "discovery.search_history_cleared",
		slog.String("action", ClearSearchHistoryAction),
		slog.String("user_id", userId.String()),
		logging.CorrelationAttr(ctx),
		slog.Time("at", at.UTC()),
	)
}
