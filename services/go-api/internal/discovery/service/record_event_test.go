package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

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
	for _, typ := range []domain.EventType{domain.EventTypeSearchPerformed} {
		store := &fakeEventStore{}
		svc := NewRecordEventService(store)
		err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), RecordEventInput{Type: typ})
		assertRejected400(t, store, err)
	}
}

func TestRecordEventService_Execute_AcceptsResultsShown(t *testing.T) {
	store := &fakeEventStore{}
	svc := NewRecordEventService(store)
	err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), RecordEventInput{
		Type:    domain.EventTypeResultsShown,
		Payload: map[string]any{"results": []any{}},
	})
	if err != nil {
		t.Fatalf("results_shown should be client-submittable, got %v", err)
	}
	if len(store.recorded()) != 1 {
		t.Errorf("appended %d events, want 1", len(store.recorded()))
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
		Type:    domain.EventTypeSkip,
		Payload: map[string]any{"dwell_ms": 1500.0, "result_signature": "sig", "session_id": "s1"},
	})
	if err != nil {
		t.Fatalf("well-typed payload rejected: %v", err)
	}
	if len(store.recorded()) != 1 {
		t.Fatalf("want 1 event appended, got %d", len(store.recorded()))
	}
}

func TestRecordEventService_Execute_RejectsPayloadOverTheSizeCap(t *testing.T) {
	store := &fakeEventStore{}
	svc := NewRecordEventService(store)
	err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), RecordEventInput{
		Type:    domain.EventTypePlay,
		Payload: map[string]any{"junk": strings.Repeat("x", maxPayloadBytes+1)},
	})
	assertRejected400(t, store, err)
}

func TestRecordEventService_Execute_RejectsPayloadOverTheKeyCap(t *testing.T) {
	store := &fakeEventStore{}
	svc := NewRecordEventService(store)
	payload := make(map[string]any, maxPayloadKeys+1)
	for i := range maxPayloadKeys + 1 {
		payload[fmt.Sprintf("k%d", i)] = 1.0
	}
	err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), RecordEventInput{
		Type:    domain.EventTypePlay,
		Payload: payload,
	})
	assertRejected400(t, store, err)
}

func TestRecordEventService_Execute_AcceptsPayloadUnderTheSizeCap(t *testing.T) {
	store := &fakeEventStore{}
	svc := NewRecordEventService(store)
	err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), RecordEventInput{
		Type:    domain.EventTypePlay,
		Payload: map[string]any{"junk": strings.Repeat("x", maxPayloadBytes-100)},
	})
	if err != nil {
		t.Fatalf("payload under the cap rejected: %v", err)
	}
	if len(store.recorded()) != 1 {
		t.Fatalf("appended %d events, want 1", len(store.recorded()))
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

func TestInvalidEventErrorCode(t *testing.T) {
	if got := (&invalidEventError{msg: "x"}).ErrorCode(); got != "discovery.invalid_event" {
		t.Errorf("code: got %q, want %q", got, "discovery.invalid_event")
	}
}

type recordingActivityFeed struct {
	mu     sync.Mutex
	events []string
}

func (r *recordingActivityFeed) EmitActivity(eventType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, eventType)
}

func (r *recordingActivityFeed) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

var _ ports.ActivityFeed = (*recordingActivityFeed)(nil)

// Overseer's usage bucket has something other than the discovery_events table
// to read.
func TestRecordEventService_EmitsActivityOnRecordedEvent(t *testing.T) {
	store := &fakeEventStore{}
	admin := &recordingActivityFeed{}
	svc := NewRecordEventService(store, WithRecordEventActivityFeed(admin))

	err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), RecordEventInput{
		Type:    domain.EventTypeLibraryAdd,
		EventId: uuid.New().String(),
		Payload: map[string]any{"track_id": "abc"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := admin.recorded(); len(got) != 1 || got[0] != "library_add" {
		t.Errorf("admin activity = %v, want [library_add]", got)
	}
}

// append never reports activity that was not actually recorded.
func TestRecordEventService_SkipsActivityOnStoreFailure(t *testing.T) {
	admin := &recordingActivityFeed{}
	svc := NewRecordEventService(failingEventStore{}, WithRecordEventActivityFeed(admin))

	err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), RecordEventInput{
		Type: domain.EventTypePlay,
	})
	if err == nil {
		t.Fatal("Execute: want error from a failing store")
	}
	if got := admin.recorded(); len(got) != 0 {
		t.Errorf("admin activity = %v, want none", got)
	}
}

func TestRecordEvent_NonClientSubmittableRenders400(t *testing.T) {
	store := &fakeEventStore{}
	svc := NewRecordEventService(store)

	err := svc.Execute(context.Background(), newUser(), RecordEventInput{
		Type: domain.EventTypeSearchPerformed,
	})
	if err == nil {
		t.Fatal("want a validation error")
	}
	var se interface{ HTTPStatus() int }
	if !errors.As(err, &se) || se.HTTPStatus() != 400 {
		t.Fatalf("error = %v, want an HTTP 400 StatusError", err)
	}
	if !strings.Contains(err.Error(), "not client-submittable") {
		t.Errorf("Error() = %q, want the not-client-submittable message", err.Error())
	}
	if len(store.recorded()) != 0 {
		t.Error("rejected event must not be appended")
	}
}
