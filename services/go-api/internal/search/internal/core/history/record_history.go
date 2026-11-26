package history

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
)

const HistoryRingSize = 100

type RecordSearchHistoryService struct {
	HistoryRepo ports.HistoryWriter
	now         clock.Func
}

var ErrCollaboratorRequired = errors.New("history services need their repository, pass the absent adapter to switch it off")

func NewRecordSearchHistoryService(historyRepo ports.HistoryWriter, now clock.Func) (*RecordSearchHistoryService, error) {
	if historyRepo == nil {
		return nil, ErrCollaboratorRequired
	}
	if now == nil {
		return nil, ErrClockRequired
	}
	return &RecordSearchHistoryService{HistoryRepo: historyRepo, now: now}, nil
}

func MustNewRecordSearchHistoryService(historyRepo ports.HistoryWriter, now clock.Func) *RecordSearchHistoryService {
	s, err := NewRecordSearchHistoryService(historyRepo, now)
	if err != nil {
		panic(err)
	}
	return s
}

func (s *RecordSearchHistoryService) Record(
	ctx context.Context,
	userId ids.UserId,
	query *domain.SearchQuery,
	queryNorm string,
	startedAt time.Time,
) {
	if err := ids.GuardNotSystem(userId); err != nil {
		return
	}
	entry := &domain.SearchHistoryEntry{
		ID:         ids.NewEntityId(),
		UserId:     userId,
		Query:      query.Raw,
		QueryNorm:  queryNorm,
		ExecutedAt: startedAt.UTC(),
	}
	if err := s.HistoryRepo.Insert(ctx, entry); err != nil {
		slog.WarnContext(ctx, "search.v2.history_persist_failed",
			"user_id", userId.String(),
			logging.SearchTextAttr(query.Raw),
			"error", logging.ScrubSearchErr(err, query.Raw))
		return
	}
	if err := s.HistoryRepo.TrimToN(ctx, userId, HistoryRingSize); err != nil {
		slog.WarnContext(ctx, "search.v2.history_trim_failed",
			"user_id", userId.String(),
			"error", err)
	}
}

func (s *RecordSearchHistoryService) RecordSubmitted(ctx context.Context, userId ids.UserId, raw string) error {
	query, err := domain.NewSearchQuery(strings.TrimSpace(raw), domain.AllKinds(), 1)
	if err != nil {
		return err
	}
	queryNorm := textnorm.NormalizeForMatch(domain.SearchText(query.Raw))
	s.Record(ctx, userId, query, queryNorm, s.now())
	return nil
}
