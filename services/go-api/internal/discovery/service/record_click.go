package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/textnorm"

	"github.com/google/uuid"
)

const clickDedupWindowSeconds = 60

type RecordClickService struct {
	clickRepo ports.SearchClickRepository
}

func NewRecordClickService(clickRepo ports.SearchClickRepository) *RecordClickService {
	return &RecordClickService{clickRepo: clickRepo}
}

type RecordClickInput struct {
	QueryNorm      string
	ResultKind     domain.ResultKind
	ResultTitle    string
	ResultSubtitle string
	Position       int
	Confidence     domain.Confidence
}

func (s *RecordClickService) Execute(ctx context.Context, userId shared.UserId, input RecordClickInput) error {
	if s.clickRepo == nil {
		return nil
	}

	signature := computeResultSignature(input.ResultKind, input.ResultTitle, input.ResultSubtitle)

	click := &domain.SearchClick{
		ID:              uuid.New(),
		UserId:          userId,
		QueryNorm:       textnorm.NormalizeForMatch(input.QueryNorm),
		ResultSignature: signature,
		Position:        input.Position,
		Confidence:      input.Confidence,
		ClickedAt:       time.Now().UTC(),
	}

	inserted, err := s.clickRepo.InsertIfOutsideWindow(ctx, click, clickDedupWindowSeconds)
	if err != nil {
		return err
	}

	if inserted {
		slog.InfoContext(ctx, "result click recorded",
			"user_id", userId.String(), "position", input.Position)
	}

	return nil
}

func computeResultSignature(kind domain.ResultKind, title, subtitle string) string {
	normTitle := textnorm.NormalizeForMatch(title)
	normSubtitle := textnorm.NormalizeForMatch(subtitle)
	input := fmt.Sprintf("%s|%s|%s", kind.String(), normTitle, normSubtitle)
	h := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", h)[:12]
}
