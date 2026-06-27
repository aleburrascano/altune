package service

import (
	"context"
	"fmt"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
)

// RecordEventService persists a client-emitted interaction event (play, skip,
// completed, library_add, wrong_album) into the telemetry store.
type RecordEventService struct {
	eventStore ports.EventStore
}

func NewRecordEventService(eventStore ports.EventStore) *RecordEventService {
	return &RecordEventService{eventStore: eventStore}
}

type RecordEventInput struct {
	Type      domain.EventType
	QueryNorm string
	SearchId  string
	Payload   map[string]any
}

func (s *RecordEventService) Execute(ctx context.Context, userId shared.UserId, input RecordEventInput) error {
	if input.Type == domain.EventTypeUnknown {
		return fmt.Errorf("record event: unknown event type")
	}

	event := domain.InteractionEvent{
		OccurredAt: time.Now().UTC(),
		UserId:     userId,
		Type:       input.Type,
		QueryNorm:  input.QueryNorm,
		SearchId:   input.SearchId,
		Payload:    input.Payload,
	}
	if err := s.eventStore.Append(ctx, event); err != nil {
		return fmt.Errorf("record event: %w", err)
	}
	return nil
}
