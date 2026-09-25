package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

const historyRingSize = 100

// RecordSearchHistoryService persists a user's executed searches as a capped
// ring. It is the write-side sibling of ListSearchHistoryService and
// ClearSearchHistoryService, pulled off Service so history persistence can
// change without touching the search orchestrator. Persistence failures are
// tolerated: a search must never fail because its history write did.
type RecordSearchHistoryService struct {
	historyRepo ports.HistoryWriter
}

func NewRecordSearchHistoryService(historyRepo ports.HistoryWriter) *RecordSearchHistoryService {
	return &RecordSearchHistoryService{historyRepo: historyRepo}
}

func (s *RecordSearchHistoryService) Record(
	ctx context.Context,
	userId shared.UserId,
	query *domain.SearchQuery,
	queryNorm string,
	saveHistory bool,
) {
	if !saveHistory || s.historyRepo == nil {
		return
	}
	if err := shared.GuardNotSystem(userId); err != nil {
		return
	}
	entry := &domain.SearchHistoryEntry{
		ID:         uuid.New(),
		UserId:     userId,
		Query:      query.Raw,
		QueryNorm:  queryNorm,
		ExecutedAt: time.Now().UTC(),
	}
	if err := s.historyRepo.Insert(ctx, entry); err != nil {
		// The search text is erasable (#1097), so the dropped write is named by
		// its fingerprint, which still joins this line to the search.v2.start
		// it belongs to (#2244).
		slog.WarnContext(ctx, "search.v2.history_persist_failed",
			"user_id", userId.String(),
			logging.SearchTextAttr(query.Raw),
			"error", logging.ScrubSearchErr(err, query.Raw))
		return
	}
	if err := s.historyRepo.TrimToN(ctx, userId, historyRingSize); err != nil {
		slog.WarnContext(ctx, "search.v2.history_trim_failed",
			"user_id", userId.String(),
			"error", err)
	}
}
