package history

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const ClearSearchHistoryAction = "clear_search_history"

type ClearSearchHistoryService struct {
	historyRepo ports.HistoryEraser
	now         clock.Func
}

var ErrClockRequired = errors.New("history: the service requires a clock")

func NewClearSearchHistoryService(historyRepo ports.HistoryEraser, now clock.Func) (*ClearSearchHistoryService, error) {
	if now == nil {
		return nil, ErrClockRequired
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
