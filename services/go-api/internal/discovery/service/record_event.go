package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type RecordEventService struct {
	eventStore ports.EventStore
	activity   ports.ActivityFeed
}

func NewRecordEventService(eventStore ports.EventStore, opts ...func(*RecordEventService)) *RecordEventService {
	s := &RecordEventService{eventStore: eventStore, activity: noopActivityFeed{}}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithRecordEventActivityFeed(activity ports.ActivityFeed) func(*RecordEventService) {
	return func(s *RecordEventService) {
		if activity != nil {
			s.activity = activity
		}
	}
}

type noopActivityFeed struct{}

func (noopActivityFeed) EmitActivity(string) {}

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

const (
	maxPayloadBytes = 8 << 10
	maxPayloadKeys  = 32
)

func validatePayloadBounds(payload map[string]any) error {
	if len(payload) > maxPayloadKeys {
		return &invalidEventError{msg: fmt.Sprintf("payload must hold at most %d keys", maxPayloadKeys)}
	}
	if !payloadStringsAreStorable(payload) {
		return &invalidEventError{msg: "payload strings must be valid UTF-8 without NUL"}
	}
	return validatePayloadSize(payload)
}

func isStorableText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

func payloadStringsAreStorable(v any) bool {
	switch t := v.(type) {
	case string:
		return isStorableText(t)
	case map[string]any:
		for k, item := range t {
			if !isStorableText(k) || !payloadStringsAreStorable(item) {
				return false
			}
		}
	case []any:
		for _, item := range t {
			if !payloadStringsAreStorable(item) {
				return false
			}
		}
	}
	return true
}

func validatePayloadSize(payload map[string]any) error {
	stored, err := json.Marshal(payload)
	if err != nil {
		return &invalidEventError{msg: "payload must be JSON-serializable"}
	}
	if len(stored) > maxPayloadBytes {
		return &invalidEventError{msg: fmt.Sprintf("payload must serialize to at most %d bytes", maxPayloadBytes)}
	}
	return nil
}

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

func requiresEventID(t domain.EventType) bool {
	switch t {
	case domain.EventTypeLibraryAdd, domain.EventTypeWrongAlbum,
		domain.EventTypeAcquisitionUi, domain.EventTypeClientError:
		return true
	}
	return false
}

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

func (s *RecordEventService) RecordAnonymousAuthFailure(ctx context.Context, reason, appVersion string) error {
	event := domain.InteractionEvent{
		OccurredAt: time.Now().UTC(),
		UserId:     shared.AnonymousUserId(),
		Type:       domain.EventTypeAuthFailed,
		Payload:    map[string]any{"reason": reason, "app_version": appVersion},
	}
	if err := s.eventStore.Append(ctx, event); err != nil {
		return fmt.Errorf("record anonymous auth failure: %w", err)
	}
	s.activity.EmitActivity(domain.EventTypeAuthFailed.String())
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
	if err := validatePayloadBounds(input.Payload); err != nil {
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
	s.activity.EmitActivity(input.Type.String())
	return nil
}
