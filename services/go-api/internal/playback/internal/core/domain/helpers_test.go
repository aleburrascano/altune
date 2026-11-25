package domain_test

import (
	"altune/go-api/internal/playback"
	shareddomain "altune/go-api/internal/shared/domain"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

const (
	maxQueueLength      = 10000
	maxQueueStringBytes = 4096
)

const (
	canonicalTrackA = "3f2504e0-4f89-11d3-9a0c-0305e82c3301"
	canonicalTrackB = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
)

func testUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

type storeSpy struct {
	stored   *playback.QueueState
	saved    *playback.QueueState
	position *playback.QueuePosition
}

func (s *storeSpy) Upsert(_ context.Context, state *playback.QueueState) error {
	s.saved = state
	return nil
}

func (s *storeSpy) UpdatePosition(_ context.Context, position *playback.QueuePosition) error {
	s.position = position
	return nil
}

func (s *storeSpy) GetForUser(context.Context, shared.UserId) (*playback.QueueState, error) {
	if s.stored == nil {
		return nil, playback.ErrQueueStateNotFound
	}
	return s.stored, nil
}

func (s *storeSpy) DeleteForUser(context.Context, shared.UserId) error {
	return nil
}

func saveQueue(in playback.SaveQueueStateInput) (*playback.QueueState, error) {
	store := &storeSpy{}
	err := playback.NewQueueService(store, nil).Save(context.Background(), testUser(), in)
	return store.saved, err
}

func savePosition(in playback.SaveQueuePositionInput) (*playback.QueuePosition, error) {
	store := &storeSpy{}
	err := playback.NewQueueService(store, nil).SavePosition(context.Background(), testUser(), in)
	return store.position, err
}

func validationCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected a validation error, got nil")
	}
	var coded *shareddomain.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error = %T (%v), want a coded validation error", err, err)
	}
	if kind := coded.Code().Kind(); kind != shareddomain.KindInvalid {
		t.Fatalf("kind = %s, want %s", kind, shareddomain.KindInvalid)
	}
	return coded.Code().String()
}
