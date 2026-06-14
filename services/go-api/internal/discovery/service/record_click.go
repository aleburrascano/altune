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
	QueryNorm       string
	ResultTitle     string
	ResultSubtitle  string
	ResultSources   []domain.SourceRef
	Position        int
	Confidence      domain.Confidence
}

func (s *RecordClickService) Execute(ctx context.Context, userId shared.UserId, input RecordClickInput) error {
	signature := computeResultSignature(input.ResultTitle, input.ResultSubtitle, input.ResultSources)

	click := &domain.SearchClick{
		ID:              uuid.New(),
		UserId:          userId,
		QueryNorm:       NormalizeForMatch(input.QueryNorm),
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

func computeResultSignature(title, subtitle string, sources []domain.SourceRef) string {
	input := fmt.Sprintf("%s|%s", title, subtitle)
	for _, s := range sources {
		input += fmt.Sprintf("|%s:%s", s.Provider.String(), s.ExternalID)
	}
	h := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", h)
}
