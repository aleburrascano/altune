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

func TestRecordEventService_Execute_WrapsStoreError(t *testing.T) {
	svc := NewRecordEventService(failingEventStore{})

	err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), RecordEventInput{
		Type: domain.EventTypePlay,
	})
	if err == nil {
		t.Fatal("expected error when the store fails")
	}
}
