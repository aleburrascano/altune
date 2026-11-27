package service

import (
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/shared/ids"
	"context"
	"fmt"
	"time"
)

type SaveQueueStateInput struct {
	TrackIds       []string
	CurrentIdx     int
	PositionMs     int64
	Shuffled       bool
	RepeatMode     string
	Source         domain.QueueSource
	LegacySourceId string
	NaturalOrder   []string
}

func (s *QueueService) Save(ctx context.Context, userId ids.UserId, input SaveQueueStateInput) error {
	state, err := newQueueState(userId, input, s.now())
	if err != nil {
		return err
	}
	return s.repo.Upsert(ctx, state)
}

func parseSourceAndRepeat(input SaveQueueStateInput) (string, domain.RepeatMode, error) {
	sourceId, err := domain.FormatQueueSource(input.Source, input.LegacySourceId)
	if err != nil {
		return "", 0, fmt.Errorf("invalid queue source: %w", err)
	}
	rm, err := domain.ParseRepeatMode(input.RepeatMode)
	if err != nil {
		return "", 0, fmt.Errorf("invalid repeat mode: %w", err)
	}
	return sourceId, rm, nil
}

func newQueueState(userId ids.UserId, input SaveQueueStateInput, now time.Time) (*domain.QueueState, error) {
	sourceId, rm, err := parseSourceAndRepeat(input)
	if err != nil {
		return nil, err
	}
	state, err := domain.NewQueueState(domain.QueueStateInput{
		UserId:       userId,
		TrackIds:     input.TrackIds,
		CurrentIdx:   input.CurrentIdx,
		PositionMs:   input.PositionMs,
		Shuffled:     input.Shuffled,
		RepeatMode:   rm,
		SourceId:     sourceId,
		NaturalOrder: input.NaturalOrder,
	}, now)
	if err != nil {
		return nil, fmt.Errorf("invalid queue state: %w", err)
	}
	return state, nil
}
