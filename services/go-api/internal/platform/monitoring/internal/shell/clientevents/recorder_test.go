package clientevents_test

import (
	"altune/go-api/internal/platform/monitoring/internal/core/domain"
	"altune/go-api/internal/platform/monitoring/internal/shell/clientevents"
	"altune/go-api/internal/shared/apierror"
	"context"
	"errors"
	"testing"
	"time"
)

type recordingStore struct {
	appended []domain.ClientEvent
	err      error
}

func (s *recordingStore) Append(_ context.Context, e domain.ClientEvent) error {
	if s.err != nil {
		return s.err
	}
	s.appended = append(s.appended, e)
	return nil
}

func (s *recordingStore) EraseRowsOfDeletedIdentities(context.Context) (int64, error) {
	return 0, nil
}

func (s *recordingStore) Prune(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func fixedNow() time.Time { return time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC) }

func TestNewRecorder_RefusesAMissingCollaborator(t *testing.T) {
	if _, err := clientevents.NewRecorder(nil, fixedNow); !errors.Is(err, clientevents.ErrCollaboratorRequired) {
		t.Errorf("no store: err = %v, want ErrCollaboratorRequired", err)
	}
	if _, err := clientevents.NewRecorder(&recordingStore{}, nil); !errors.Is(err, clientevents.ErrCollaboratorRequired) {
		t.Errorf("no clock: err = %v, want ErrCollaboratorRequired", err)
	}
}

func TestRecorder_StampsTheServerTimeAndStoresAValidEvent(t *testing.T) {
	store := &recordingStore{}
	recorder, err := clientevents.NewRecorder(store, fixedNow)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	if err := recorder.Record(context.Background(), domain.ClientEvent{Type: domain.ClientEventScreenView}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	if len(store.appended) != 1 || !store.appended[0].OccurredAt.Equal(fixedNow()) {
		t.Errorf("appended = %+v, want one event stamped with the server clock", store.appended)
	}
}

func TestRecorder_AnInvalidEventIsRefusedAndNeverStored(t *testing.T) {
	store := &recordingStore{}
	recorder, _ := clientevents.NewRecorder(store, fixedNow)

	err := recorder.Record(context.Background(), domain.ClientEvent{Type: domain.ClientEventClientError})

	var coded *apierror.CodedError
	if !errors.As(err, &coded) || coded.Code() != apierror.CodeDiscoveryInvalidEvent {
		t.Fatalf("err = %v, want the invalid-event code", err)
	}
	if len(store.appended) != 0 {
		t.Errorf("appended = %+v, want nothing stored", store.appended)
	}
}

func TestRecorder_AStoreFailureIsReturned(t *testing.T) {
	boom := errors.New("down")
	recorder, _ := clientevents.NewRecorder(&recordingStore{err: boom}, fixedNow)

	err := recorder.Record(context.Background(), domain.ClientEvent{Type: domain.ClientEventScreenView})

	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want the store's error wrapped", err)
	}
}
