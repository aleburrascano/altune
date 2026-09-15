package service

import (
	"context"
	"fmt"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
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
	for _, key := range [...]string{"dwell_ms", "tail_noise_top5"} {
		if v, ok := payload[key]; ok {
			if _, isNum := v.(float64); !isNum {
				return &invalidEventError{msg: fmt.Sprintf("payload.%s must be a number", key)}
			}
		}
	}
	if v, ok := payload["zero_result"]; ok {
		if _, isBool := v.(bool); !isBool {
			return &invalidEventError{msg: "payload.zero_result must be a boolean"}
		}
	}
	for _, key := range [...]string{"result_signature", "session_id"} {
		if v, ok := payload[key]; ok {
			if _, isStr := v.(string); !isStr {
				return &invalidEventError{msg: fmt.Sprintf("payload.%s must be a string", key)}
			}
		}
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
