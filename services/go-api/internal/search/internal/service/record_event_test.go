package service_test

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/search/searchtest"
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/clock/clocktest"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewRecordEventService_RefusesAMissingCollaborator(t *testing.T) {
	tests := []struct {
		name  string
		store ports.EventStore
		feed  ports.ActivityFeed
	}{
		{"event store", nil, service.NoopActivityFeed{}},
		{"activity feed", &searchtest.FakeEventStore{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := service.NewRecordEventService(tt.store, tt.feed, clock.Now)

			if svc != nil || !errors.Is(err, service.ErrActivityCollaboratorRequired) {
				t.Fatalf("got (%v, %v), want a refusal wrapping ErrCollaboratorRequired", svc, err)
			}
		})
	}
}

const (
	payloadByteCap = 8 << 10
	payloadKeyCap  = 32
)

type failingEventStore struct{}

func (failingEventStore) Append(context.Context, domain.InteractionEvent) error {
	return errors.New("db down")
}

func TestRecordEventService_Execute_AppendsEvent(t *testing.T) {
	store := &searchtest.FakeEventStore{}
	svc := searchtest.NewRecordEvent(t, store)
	userId := ids.NewUserId(uuid.New())

	err := svc.Execute(context.Background(), userId, service.RecordEventInput{
		Type:    domain.InteractionTypePlay,
		Payload: map[string]any{"video_id": "abc"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	events := store.Recorded()
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	if events[0].Type != domain.InteractionTypePlay {
		t.Errorf("type = %s, want play", events[0].Type)
	}
	if events[0].UserId != userId {
		t.Errorf("user_id mismatch")
	}
}

func TestRecordEventService_Execute_ThreadsSearchId(t *testing.T) {
	store := &searchtest.FakeEventStore{}
	svc := searchtest.NewRecordEvent(t, store)
	userId := ids.NewUserId(uuid.New())
	searchId := uuid.New().String()

	err := svc.Execute(context.Background(), userId, service.RecordEventInput{
		Type:     domain.InteractionTypeResultClicked,
		SearchId: searchId,
		Payload:  map[string]any{"position": 0},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	events := store.Recorded()
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	if events[0].SearchId != searchId {
		t.Errorf("search_id = %q, want %q", events[0].SearchId, searchId)
	}
}

func TestRecordEventService_Execute_RejectsUnknownType(t *testing.T) {
	store := &searchtest.FakeEventStore{}
	svc := searchtest.NewRecordEvent(t, store)

	err := svc.Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{
		Type: domain.InteractionTypeUnknown,
	})
	if err == nil {
		t.Fatal("expected error for unknown event type")
	}
	if len(store.Recorded()) != 0 {
		t.Error("nothing should be appended for an unknown type")
	}
}

func assertRejected400(t *testing.T, store *searchtest.FakeEventStore, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a validation error")
	}
	var coded *apierror.CodedError
	if !errors.As(err, &coded) || coded.Code().Kind() != apierror.KindInvalid {
		t.Errorf("error should be an invalid-input error, got %v", err)
	}
	if len(store.Recorded()) != 0 {
		t.Error("nothing should be appended for a rejected event")
	}
}

func TestRecordEventService_Execute_RejectsServerReservedTypes(t *testing.T) {
	for _, typ := range []domain.InteractionType{domain.InteractionTypeSearchPerformed} {
		store := &searchtest.FakeEventStore{}
		svc := searchtest.NewRecordEvent(t, store)
		err := svc.Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{Type: typ})
		assertRejected400(t, store, err)
	}
}

func TestRecordEventService_Execute_AcceptsResultsShown(t *testing.T) {
	store := &searchtest.FakeEventStore{}
	svc := searchtest.NewRecordEvent(t, store)
	err := svc.Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{
		Type:    domain.InteractionTypeResultsShown,
		Payload: map[string]any{"results": []any{}},
	})
	if err != nil {
		t.Fatalf("results_shown should be client-submittable, got %v", err)
	}
	if len(store.Recorded()) != 1 {
		t.Errorf("appended %d events, want 1", len(store.Recorded()))
	}
}

func TestRecordEventService_Execute_RejectsNonNumericDwell(t *testing.T) {
	store := &searchtest.FakeEventStore{}
	svc := searchtest.NewRecordEvent(t, store)
	err := svc.Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{
		Type:    domain.InteractionTypeSkip,
		Payload: map[string]any{"dwell_ms": "abc"},
	})
	assertRejected400(t, store, err)
}

func TestRecordEventService_Execute_RejectsNonBooleanZeroResult(t *testing.T) {
	store := &searchtest.FakeEventStore{}
	svc := searchtest.NewRecordEvent(t, store)
	err := svc.Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{
		Type:    domain.InteractionTypePlay,
		Payload: map[string]any{"zero_result": "false"},
	})
	assertRejected400(t, store, err)
}

func TestRecordEventService_Execute_RejectsNonStringSignatureAndSession(t *testing.T) {
	for _, key := range []string{"result_signature", "session_id"} {
		store := &searchtest.FakeEventStore{}
		svc := searchtest.NewRecordEvent(t, store)
		err := svc.Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{
			Type:    domain.InteractionTypePlay,
			Payload: map[string]any{key: 42.0},
		})
		assertRejected400(t, store, err)
	}
}

func TestRecordEventService_Execute_AcceptsWellTypedPayload(t *testing.T) {
	store := &searchtest.FakeEventStore{}
	svc := searchtest.NewRecordEvent(t, store)
	err := svc.Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{
		Type:    domain.InteractionTypeSkip,
		Payload: map[string]any{"dwell_ms": 1500.0, "result_signature": "sig", "session_id": "s1"},
	})
	if err != nil {
		t.Fatalf("well-typed payload rejected: %v", err)
	}
	if len(store.Recorded()) != 1 {
		t.Fatalf("want 1 event appended, got %d", len(store.Recorded()))
	}
}

func TestRecordEventService_Execute_RejectsPayloadOverTheSizeCap(t *testing.T) {
	store := &searchtest.FakeEventStore{}
	svc := searchtest.NewRecordEvent(t, store)
	err := svc.Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{
		Type:    domain.InteractionTypePlay,
		Payload: map[string]any{"junk": strings.Repeat("x", payloadByteCap+1)},
	})
	assertRejected400(t, store, err)
}

func TestRecordEventService_Execute_RejectsPayloadOverTheKeyCap(t *testing.T) {
	store := &searchtest.FakeEventStore{}
	svc := searchtest.NewRecordEvent(t, store)
	payload := make(map[string]any, payloadKeyCap+1)
	for i := range payloadKeyCap + 1 {
		payload[fmt.Sprintf("k%d", i)] = 1.0
	}
	err := svc.Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{
		Type:    domain.InteractionTypePlay,
		Payload: payload,
	})
	assertRejected400(t, store, err)
}

func TestRecordEventService_Execute_AcceptsPayloadAtTheKeyCap(t *testing.T) {
	store := &searchtest.FakeEventStore{}
	payload := make(map[string]any, payloadKeyCap)
	for i := range payloadKeyCap {
		payload[fmt.Sprintf("k%d", i)] = 1.0
	}
	err := searchtest.NewRecordEvent(t, store).Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{
		Type:    domain.InteractionTypePlay,
		Payload: payload,
	})
	if err != nil {
		t.Fatalf("payload at the key cap rejected: %v", err)
	}
	if len(store.Recorded()) != 1 {
		t.Fatalf("appended %d events, want 1", len(store.Recorded()))
	}
}

func TestRecordEventService_Execute_RejectsNULOrInvalidUTF8InPayloadStrings(t *testing.T) {
	cases := map[string]map[string]any{
		"top-level value": {"result_signature": "a\x00b"},
		"key":             {"k\x00": 1.0},
		"nested value":    {"extra": map[string]any{"deep": []any{"a\x00"}}},
		"invalid UTF-8":   {"session_id": "a\xffb"},
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			store := &searchtest.FakeEventStore{}
			err := searchtest.NewRecordEvent(t, store).Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{
				Type:    domain.InteractionTypePlay,
				Payload: payload,
			})
			assertRejected400(t, store, err)
		})
	}
}

func TestRecordEventService_Execute_AcceptsPayloadUnderTheSizeCap(t *testing.T) {
	store := &searchtest.FakeEventStore{}
	svc := searchtest.NewRecordEvent(t, store)
	err := svc.Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{
		Type:    domain.InteractionTypePlay,
		Payload: map[string]any{"junk": strings.Repeat("x", payloadByteCap-100)},
	})
	if err != nil {
		t.Fatalf("payload under the cap rejected: %v", err)
	}
	if len(store.Recorded()) != 1 {
		t.Fatalf("appended %d events, want 1", len(store.Recorded()))
	}
}

func TestRecordEventService_Execute_SizeCapCountsTheSerializedPayloadBytes(t *testing.T) {
	envelope := len(`{"junk":""}`)
	for _, tc := range []struct {
		name     string
		bytes    int
		accepted bool
	}{
		{"exactly at the cap", payloadByteCap, true},
		{"one byte over the cap", payloadByteCap + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &searchtest.FakeEventStore{}
			err := searchtest.NewRecordEvent(t, store).Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{
				Type:    domain.InteractionTypePlay,
				Payload: map[string]any{"junk": strings.Repeat("x", tc.bytes-envelope)},
			})
			if !tc.accepted {
				assertRejected400(t, store, err)
				return
			}
			if err != nil || len(store.Recorded()) != 1 {
				t.Fatalf("payload of %d serialized bytes: err %v, appended %d, want accepted", tc.bytes, err, len(store.Recorded()))
			}
		})
	}
}

func TestRecordEventService_Execute_WrapsStoreError(t *testing.T) {
	svc := searchtest.NewRecordEvent(t, failingEventStore{})

	err := svc.Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{
		Type: domain.InteractionTypePlay,
	})
	if err == nil {
		t.Fatal("expected error when the store fails")
	}
}

func TestInvalidEventLookAlikeIsNotCoded(t *testing.T) {
	var coded *apierror.CodedError
	if errors.As(errors.New("discovery.invalid_event"), &coded) {
		t.Error("a plain error whose text is the code must not match *apierror.CodedError")
	}
	if !errors.As(domain.NewInvalidEventError("x"), &coded) || coded.Code().String() != "discovery.invalid_event" {
		t.Errorf("NewInvalidEventError should match *apierror.CodedError with code discovery.invalid_event, got %v", coded)
	}
}

var _ ports.ActivityFeed = (*searchtest.RecordingActivityFeed)(nil)

func TestRecordEventService_EmitsActivityOnRecordedEvent(t *testing.T) {
	store := &searchtest.FakeEventStore{}
	feed := &searchtest.RecordingActivityFeed{}
	svc := searchtest.NewRecordEventWithFeed(t, store, feed)

	err := svc.Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{
		Type:    domain.InteractionTypeLibraryAdd,
		EventId: uuid.New().String(),
		Payload: map[string]any{"track_id": "abc"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := feed.Recorded(); len(got) != 1 || got[0] != "library_add" {
		t.Errorf("feed activity = %v, want [library_add]", got)
	}
}

func TestRecordEventService_SkipsActivityOnStoreFailure(t *testing.T) {
	feed := &searchtest.RecordingActivityFeed{}
	svc := searchtest.NewRecordEventWithFeed(t, failingEventStore{}, feed)

	err := svc.Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{
		Type: domain.InteractionTypePlay,
	})
	if err == nil {
		t.Fatal("Execute: want error from a failing store")
	}
	if got := feed.Recorded(); len(got) != 0 {
		t.Errorf("feed activity = %v, want none", got)
	}
}

func TestRecordEvent_NonClientSubmittableRenders400(t *testing.T) {
	store := &searchtest.FakeEventStore{}
	svc := searchtest.NewRecordEvent(t, store)

	err := svc.Execute(context.Background(), searchtest.NewUser(), service.RecordEventInput{
		Type: domain.InteractionTypeSearchPerformed,
	})
	if err == nil {
		t.Fatal("want a validation error")
	}
	var coded *apierror.CodedError
	if !errors.As(err, &coded) || coded.Code().Kind() != apierror.KindInvalid {
		t.Fatalf("error = %v, want an invalid-input error", err)
	}
	if !strings.Contains(err.Error(), "not client-submittable") {
		t.Errorf("Error() = %q, want the not-client-submittable message", err.Error())
	}
	if len(store.Recorded()) != 0 {
		t.Error("rejected event must not be appended")
	}
}

func TestRecordEventService_Execute_RequiresEventIdForAcquisitionUiAndClientError(t *testing.T) {
	for _, eventType := range []domain.InteractionType{domain.InteractionTypeAcquisitionUi, domain.InteractionTypeClientError} {
		t.Run(eventType.String(), func(t *testing.T) {
			store := &searchtest.FakeEventStore{}
			svc := searchtest.NewRecordEvent(t, store)

			err := svc.Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{
				Type:    eventType,
				Payload: map[string]any{},
			})
			if err == nil {
				t.Fatalf("expected an error for a missing event_id on %s", eventType)
			}
			if !strings.Contains(err.Error(), "event_id is required") {
				t.Errorf("error = %q, want it to mention event_id", err.Error())
			}

			err = svc.Execute(context.Background(), ids.NewUserId(uuid.New()), service.RecordEventInput{
				Type:    eventType,
				EventId: uuid.New().String(),
				Payload: map[string]any{},
			})
			if err != nil {
				t.Fatalf("Execute with an event_id: %v", err)
			}
		})
	}
}

func TestNewRecordEventService_RefusesANilClock(t *testing.T) {
	svc, err := service.NewRecordEventService(&searchtest.FakeEventStore{}, service.NoopActivityFeed{}, nil)

	if svc != nil || !errors.Is(err, service.ErrActivityCollaboratorRequired) {
		t.Fatalf("got (%v, %v), want a refusal wrapping ErrCollaboratorRequired", svc, err)
	}
}

func TestRecordEventService_StampsEventsFromTheInjectedClock(t *testing.T) {
	start := time.Date(2026, time.March, 4, 10, 30, 0, 0, time.UTC)
	manual := clocktest.NewManual(start)
	store := &searchtest.FakeEventStore{}
	svc, err := service.NewRecordEventService(store, service.NoopActivityFeed{}, manual.Func())
	if err != nil {
		t.Fatalf("NewRecordEventService: %v", err)
	}

	if err := svc.Execute(t.Context(), ids.NewUserId(uuid.New()), service.RecordEventInput{Type: domain.InteractionTypePlay}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	manual.Advance(90 * time.Second)
	if err := svc.RecordAnonymousAuthFailure(t.Context(), "expired", "1.2.3"); err != nil {
		t.Fatalf("RecordAnonymousAuthFailure: %v", err)
	}

	events := store.Recorded()
	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d", len(events))
	}
	if !events[0].OccurredAt.Equal(start) {
		t.Errorf("first event OccurredAt = %v, want %v", events[0].OccurredAt, start)
	}
	if want := start.Add(90 * time.Second); !events[1].OccurredAt.Equal(want) {
		t.Errorf("auth failure OccurredAt = %v, want %v", events[1].OccurredAt, want)
	}
}
