package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"log/slog"
	"strings"
	"time"
)

const historyRingSize = 100

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
	startedAt time.Time,
) {
	if !saveHistory || s.historyRepo == nil {
		return
	}
	if err := shared.GuardNotSystem(userId); err != nil {
		return
	}
	entry := &domain.SearchHistoryEntry{
		ID:         shared.NewEntityId(),
		UserId:     userId,
		Query:      query.Raw,
		QueryNorm:  queryNorm,
		ExecutedAt: startedAt.UTC(),
	}
	if err := s.historyRepo.Insert(ctx, entry); err != nil {
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

func (s *RecordSearchHistoryService) RecordSubmitted(ctx context.Context, userId shared.UserId, raw string) error {
	query, err := domain.NewSearchQuery(strings.TrimSpace(raw), domain.AllKinds(), 1)
	if err != nil {
		return err
	}
	queryNorm := textnorm.NormalizeForMatch(CleanQuery(query.Raw))
	s.Record(ctx, userId, query, queryNorm, true, time.Now())
	return nil
}
