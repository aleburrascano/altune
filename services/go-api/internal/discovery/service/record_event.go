package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type RecordEventService struct {
	eventStore ports.EventStore
}

func NewRecordEventService(eventStore ports.EventStore) *RecordEventService {
	return &RecordEventService{eventStore: eventStore}
}

// RecordEventInput carries no query_norm: a client-submitted event's query is
// whatever its search_id's server-emitted search_performed row says, resolved
// by the EventStore, never a client-chosen value (#1086).
type RecordEventInput struct {
	Type             domain.EventType
	SearchId         string
	EventId          string
	ClientOccurredAt time.Time
	Payload          map[string]any
}

type invalidEventError struct{ msg string }

func (e *invalidEventError) Error() string     { return e.msg }
func (e *invalidEventError) HTTPStatus() int   { return 400 }
func (e *invalidEventError) ErrorCode() string { return "discovery.invalid_event" }

func validatePayloadTypes(payload map[string]any) error {
	for _, key := range [...]string{domain.PayloadKeyDwellMs, domain.PayloadKeyTailNoiseTop5} {
		if v, ok := payload[key]; ok {
			if _, isNum := v.(float64); !isNum {
				return &invalidEventError{msg: fmt.Sprintf("payload.%s must be a number", key)}
			}
		}
	}
	if v, ok := payload[domain.PayloadKeyZeroResult]; ok {
		if _, isBool := v.(bool); !isBool {
			return &invalidEventError{msg: fmt.Sprintf("payload.%s must be a boolean", domain.PayloadKeyZeroResult)}
		}
	}
	for _, key := range [...]string{domain.PayloadKeyResultSignature, domain.PayloadKeySessionId} {
		if v, ok := payload[key]; ok {
			if _, isStr := v.(string); !isStr {
				return &invalidEventError{msg: fmt.Sprintf("payload.%s must be a string", key)}
			}
		}
	}
	return nil
}

// requiresEventID reports whether a type belongs to the label-critical tier the
// mobile outbox delivers at least once. Its retries are only safe no-ops if they
// carry the same event_id, because a NULL event_id never hits the dedup index.
// play/skip/completed stay fire-and-forget: the client sends them without an
// event_id, so requiring one would silently drop all playback signals.
func requiresEventID(t domain.EventType) bool {
	return t == domain.EventTypeLibraryAdd || t == domain.EventTypeWrongAlbum
}

// validateEventID rejects an event_id that could not dedup: a present but
// unparseable or nil UUID for any type, and a missing one for the critical tier.
func validateEventID(t domain.EventType, eventID string) error {
	if eventID == "" {
		if requiresEventID(t) {
			return &invalidEventError{msg: fmt.Sprintf("event_id is required for %q events", t)}
		}
		return nil
	}
	if id, err := uuid.Parse(eventID); err != nil || id == uuid.Nil {
		return &invalidEventError{msg: "event_id must be a non-nil UUID"}
	}
	return nil
}

func (s *RecordEventService) Execute(ctx context.Context, userId shared.UserId, input RecordEventInput) error {
	if input.Type == domain.EventTypeUnknown {
		return fmt.Errorf("record event: unknown event type")
	}
	if !input.Type.ClientSubmittable() {
		return &invalidEventError{msg: fmt.Sprintf("event type %q is not client-submittable", input.Type)}
	}
	if err := validateEventID(input.Type, input.EventId); err != nil {
		return err
	}
	if err := validatePayloadTypes(input.Payload); err != nil {
		return err
	}

	event := domain.InteractionEvent{
		OccurredAt:       time.Now().UTC(),
		UserId:           userId,
		Type:             input.Type,
		SearchId:         input.SearchId,
		EventId:          input.EventId,
		ClientOccurredAt: input.ClientOccurredAt,
		Payload:          input.Payload,
	}
	if err := s.eventStore.Append(ctx, event); err != nil {
		return fmt.Errorf("record event: %w", err)
	}
	return nil
}
