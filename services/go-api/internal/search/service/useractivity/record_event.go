package useractivity

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

type RecordEventService struct {
	eventStore ports.EventStore
	activity   ports.ActivityFeed
}

var ErrCollaboratorRequired = errors.New("record event service needs its event store and activity feed, pass the noop feed to switch it off")

func NewRecordEventService(eventStore ports.EventStore, activity ports.ActivityFeed) (*RecordEventService, error) {
	if eventStore == nil || activity == nil {
		return nil, ErrCollaboratorRequired
	}
	return &RecordEventService{eventStore: eventStore, activity: activity}, nil
}

type NoopActivityFeed struct{}

func (NoopActivityFeed) EmitActivity(string) {}

type RecordEventInput struct {
	Type             domain.EventType
	SearchId         string
	EventId          string
	ClientOccurredAt time.Time
	Payload          map[string]any
}

const (
	maxPayloadBytes = 8 << 10
	maxPayloadKeys  = 32
)

func validatePayloadBounds(payload map[string]any) error {
	if len(payload) > maxPayloadKeys {
		return domain.NewInvalidEventError(fmt.Sprintf("payload must hold at most %d keys", maxPayloadKeys))
	}
	if !payloadStringsAreStorable(payload) {
		return domain.NewInvalidEventError("payload strings must be valid UTF-8 without NUL")
	}
	return validatePayloadSize(payload)
}

func payloadStringsAreStorable(v any) bool {
	switch t := v.(type) {
	case string:
		return domain.IsStorableText(t)
	case map[string]any:
		for k, item := range t {
			if !domain.IsStorableText(k) || !payloadStringsAreStorable(item) {
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
		return domain.NewInvalidEventError("payload must be JSON-serializable")
	}
	if len(stored) > maxPayloadBytes {
		return domain.NewInvalidEventError(fmt.Sprintf("payload must serialize to at most %d bytes", maxPayloadBytes))
	}
	return nil
}

func validatePayloadTypes(payload map[string]any) error {
	for _, key := range [...]string{domain.PayloadKeyDwellMs, domain.PayloadKeyTailNoiseTop5} {
		if v, ok := payload[key]; ok {
			if _, isNum := v.(float64); !isNum {
				return domain.NewInvalidEventError(fmt.Sprintf("payload.%s must be a number", key))
			}
		}
	}
	if v, ok := payload[domain.PayloadKeyZeroResult]; ok {
		if _, isBool := v.(bool); !isBool {
			return domain.NewInvalidEventError(fmt.Sprintf("payload.%s must be a boolean", domain.PayloadKeyZeroResult))
		}
	}
	for _, key := range [...]string{domain.PayloadKeyResultSignature, domain.PayloadKeySessionId} {
		if v, ok := payload[key]; ok {
			if _, isStr := v.(string); !isStr {
				return domain.NewInvalidEventError(fmt.Sprintf("payload.%s must be a string", key))
			}
		}
	}
	return nil
}

func requiresEventID(t domain.EventType) bool {
	return slices.Contains(eventTypesRequiringID, t)
}

var eventTypesRequiringID = []domain.EventType{
	domain.EventTypeLibraryAdd, domain.EventTypeWrongAlbum,
	domain.EventTypeAcquisitionUi, domain.EventTypeClientError,
}

func validateEventID(t domain.EventType, eventID string) error {
	if eventID == "" {
		if requiresEventID(t) {
			return domain.NewInvalidEventError(fmt.Sprintf("event_id is required for %q events", t))
		}
		return nil
	}
	if id, err := ids.ParseEntityId(eventID); err != nil || id.IsZero() {
		return domain.NewInvalidEventError("event_id must be a non-nil UUID")
	}
	return nil
}

func (s *RecordEventService) RecordAnonymousAuthFailure(ctx context.Context, reason, appVersion string) error {
	event := domain.InteractionEvent{
		OccurredAt: time.Now().UTC(),
		UserId:     ids.AnonymousUserId(),
		Type:       domain.EventTypeAuthFailed,
		Payload:    map[string]any{"reason": reason, "app_version": appVersion},
	}
	if err := s.eventStore.Append(ctx, event); err != nil {
		return fmt.Errorf("record anonymous auth failure: %w", err)
	}
	s.activity.EmitActivity(domain.EventTypeAuthFailed.String())
	return nil
}

func (s *RecordEventService) Execute(ctx context.Context, userId ids.UserId, input RecordEventInput) error {
	if input.Type == domain.EventTypeUnknown {
		return fmt.Errorf("record event: unknown event type")
	}
	if !input.Type.ClientSubmittable() {
		return domain.NewInvalidEventError(fmt.Sprintf("event type %q is not client-submittable", input.Type))
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
