package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

type failingEventStore struct{}

func (failingEventStore) Append(context.Context, domain.InteractionEvent) error {
	return errors.New("db down")
}

func TestRecordEventService_Execute_AppendsEvent(t *testing.T) {
	store := &fakeEventStore{}
	svc := NewRecordEventService(store)
	userId := shared.NewUserId(uuid.New())

	err := svc.Execute(context.Background(), userId, RecordEventInput{
		Type:    domain.EventTypePlay,
		Payload: map[string]any{"video_id": "abc"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	events := store.recorded()
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	if events[0].Type != domain.EventTypePlay {
		t.Errorf("type = %s, want play", events[0].Type)
	}
	if events[0].UserId != userId {
		t.Errorf("user_id mismatch")
	}
}

func TestRecordEventService_Execute_ThreadsSearchId(t *testing.T) {
	store := &fakeEventStore{}
	svc := NewRecordEventService(store)
	userId := shared.NewUserId(uuid.New())
	searchId := uuid.New().String()

	err := svc.Execute(context.Background(), userId, RecordEventInput{
		Type:     domain.EventTypeResultClicked,
		SearchId: searchId,
		Payload:  map[string]any{"position": 0},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	events := store.recorded()
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	if events[0].SearchId != searchId {
		t.Errorf("search_id = %q, want %q", events[0].SearchId, searchId)
	}
}

func TestRecordEventService_Execute_RejectsUnknownType(t *testing.T) {
	store := &fakeEventStore{}
	svc := NewRecordEventService(store)

	err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), RecordEventInput{
		Type: domain.EventTypeUnknown,
	})
	if err == nil {
		t.Fatal("expected error for unknown event type")
	}
	if len(store.recorded()) != 0 {
		t.Error("nothing should be appended for an unknown type")
	}
}

// assertRejected400 asserts err is a 400-status validation error (via the
// structural httputil.StatusError contract) and that nothing was appended.
func assertRejected400(t *testing.T, store *fakeEventStore, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a validation error")
	}
	var se interface{ HTTPStatus() int }
	if !errors.As(err, &se) || se.HTTPStatus() != 400 {
		t.Errorf("error should carry HTTP 400, got %v", err)
	}
	if len(store.recorded()) != 0 {
		t.Error("nothing should be appended for a rejected event")
	}
}

func TestRecordEventService_Execute_RejectsServerReservedTypes(t *testing.T) {
	for _, typ := range []domain.EventType{domain.EventTypeSearchPerformed, domain.EventTypeResultsShown} {
		store := &fakeEventStore{}
		svc := NewRecordEventService(store)
		err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), RecordEventInput{Type: typ})
		assertRejected400(t, store, err)
	}
}

func TestRecordEventService_Execute_RejectsNonNumericDwell(t *testing.T) {
	store := &fakeEventStore{}
	svc := NewRecordEventService(store)
	err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), RecordEventInput{
		Type:    domain.EventTypeSkip,
		Payload: map[string]any{"dwell_ms": "abc"},
	})
	assertRejected400(t, store, err)
}

func TestRecordEventService_Execute_RejectsNonBooleanZeroResult(t *testing.T) {
	store := &fakeEventStore{}
	svc := NewRecordEventService(store)
	err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), RecordEventInput{
		Type:    domain.EventTypePlay,
		Payload: map[string]any{"zero_result": "false"},
	})
	assertRejected400(t, store, err)
}

func TestRecordEventService_Execute_RejectsNonStringSignatureAndSession(t *testing.T) {
	for _, key := range []string{"result_signature", "session_id"} {
		store := &fakeEventStore{}
		svc := NewRecordEventService(store)
		err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), RecordEventInput{
			Type:    domain.EventTypePlay,
			Payload: map[string]any{key: 42.0},
		})
		assertRejected400(t, store, err)
	}
}

func TestRecordEventService_Execute_AcceptsWellTypedPayload(t *testing.T) {
	store := &fakeEventStore{}
	svc := NewRecordEventService(store)
	err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), RecordEventInput{
		Type: domain.EventTypeSkip,
		// float64 is what encoding/json hands the handler for any JSON number.
		Payload: map[string]any{"dwell_ms": 1500.0, "result_signature": "sig", "session_id": "s1"},
	})
	if err != nil {
		t.Fatalf("well-typed payload rejected: %v", err)
	}
	if len(store.recorded()) != 1 {
		t.Fatalf("want 1 event appended, got %d", len(store.recorded()))
	}
}

func TestRecordEventService_Execute_WrapsStoreError(t *testing.T) {
	svc := NewRecordEventService(failingEventStore{})

	err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), RecordEventInput{
		Type: domain.EventTypePlay,
	})
	if err == nil {
		t.Fatal("expected error when the store fails")
	}
}
