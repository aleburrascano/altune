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
	Type             domain.EventType
	QueryNorm        string
	SearchId         string
	EventId          string
	ClientOccurredAt time.Time
	Payload          map[string]any
}

// invalidEventError renders as HTTP 400 via the structural httputil.StatusError
// interface (no net/http import in the application layer).
type invalidEventError struct{ msg string }

func (e *invalidEventError) Error() string   { return e.msg }
func (e *invalidEventError) HTTPStatus() int { return 400 }

// validatePayloadTypes rejects known payload keys carrying the wrong JSON type.
// The read-side aggregate SQL guards its casts with jsonb_typeof and silently
// skips poisoned rows; this ingest gate is the other half of the fix — a
// malformed event is refused with a 400 instead of landing as an aggregate-dark
// row. Unknown keys pass untouched (the payload is deliberately open-schema).
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
	// Clients may only submit interaction types; the server-emitted envelope
	// types (search_performed, results_shown) would poison coverage/CTR
	// aggregates if minted client-side. Server emitters append to the EventStore
	// directly and never pass through this use case.
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
		QueryNorm:        input.QueryNorm,
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
