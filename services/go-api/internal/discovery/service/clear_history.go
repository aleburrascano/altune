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

// ClearSearchHistoryAction names the erasure in audit records so the success
// and failure lines for the same action can be joined.
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

// auditHistoryCleared records who erased their search history and when
// (#1101); the row delete is otherwise the only trace. Clearing history is a
// privacy action, so the erased search text is never logged (#1097).
func auditHistoryCleared(ctx context.Context, userId shared.UserId) {
	slog.InfoContext(ctx, "discovery.search_history_cleared",
		slog.String("action", ClearSearchHistoryAction),
		slog.String("user_id", userId.String()),
		logging.CorrelationAttr(ctx),
		slog.Time("at", time.Now().UTC()),
	)
}
