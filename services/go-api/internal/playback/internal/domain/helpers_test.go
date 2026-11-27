package domain_test

import (
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/playback/internal/service"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"testing"
	"time"

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

func testUser() ids.UserId {
	return ids.NewUserId(uuid.New())
}

type storeSpy struct {
	stored   *domain.QueueState
	saved    *domain.QueueState
	position *domain.QueuePosition
}

func (s *storeSpy) Upsert(_ context.Context, state *domain.QueueState) error {
	s.saved = state
	return nil
}

func (s *storeSpy) UpdatePosition(_ context.Context, position *domain.QueuePosition) error {
	s.position = position
	return nil
}

func (s *storeSpy) GetForUser(context.Context, ids.UserId) (*domain.QueueState, error) {
	if s.stored == nil {
		return nil, domain.ErrQueueStateNotFound
	}
	return s.stored, nil
}

func (s *storeSpy) DeleteForUser(context.Context, ids.UserId) error {
	return nil
}

func saveQueue(in service.SaveQueueStateInput) (*domain.QueueState, error) {
	store := &storeSpy{}
	err := service.MustNewQueueService(store, nil, time.Now).Save(context.Background(), testUser(), in)
	return store.saved, err
}

func savePosition(in service.SaveQueuePositionInput) (*domain.QueuePosition, error) {
	store := &storeSpy{}
	err := service.MustNewQueueService(store, nil, time.Now).SavePosition(context.Background(), testUser(), in)
	return store.position, err
}

func validationCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected a validation error, got nil")
	}
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %T (%v), want a domain validation error", err, err)
	}
	return "playback." + string(invalid.Kind)
}
