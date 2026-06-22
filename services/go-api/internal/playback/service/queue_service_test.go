package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/shared"
)

// inMemoryQueueRepo is an in-memory implementation of ports.QueueStateRepository
// for testing the use case without a database.
type inMemoryQueueRepo struct {
	states map[uuid.UUID]*domain.QueueState
}

func newInMemoryQueueRepo() *inMemoryQueueRepo {
	return &inMemoryQueueRepo{states: map[uuid.UUID]*domain.QueueState{}}
}

func (r *inMemoryQueueRepo) Upsert(_ context.Context, state *domain.QueueState) error {
	r.states[state.UserId.UUID()] = state
	return nil
}

func (r *inMemoryQueueRepo) GetForUser(_ context.Context, userId shared.UserId) (*domain.QueueState, error) {
	return r.states[userId.UUID()], nil
}

func testUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

func TestQueueService_Save_PersistsValidState(t *testing.T) {
	repo := newInMemoryQueueRepo()
	svc := NewQueueService(repo)
	user := testUser()

	err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:   []string{"a", "b", "c"},
		CurrentIdx: 2,
		PositionMs: 1000,
		RepeatMode: "all",
		SourceId:   "library",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := repo.GetForUser(context.Background(), user)
	if got == nil {
		t.Fatal("expected state to be persisted")
	}
	if got.CurrentIdx != 2 || got.RepeatMode != domain.RepeatAll {
		t.Errorf("persisted state mismatch: idx=%d repeat=%v", got.CurrentIdx, got.RepeatMode)
	}
}

func TestQueueService_Save_RejectsInvalidRepeatMode(t *testing.T) {
	svc := NewQueueService(newInMemoryQueueRepo())
	err := svc.Save(context.Background(), testUser(), SaveQueueStateInput{
		TrackIds:   []string{"a"},
		RepeatMode: "bogus",
	})
	if err == nil {
		t.Fatal("expected invalid repeat mode to be rejected")
	}
}

func TestQueueService_Resume_ReturnsEmptyWhenNoneStored(t *testing.T) {
	svc := NewQueueService(newInMemoryQueueRepo())

	state, err := svc.Resume(context.Background(), testUser())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state == nil {
		t.Fatal("Resume must never return nil")
	}
	if len(state.TrackIds) != 0 || state.RepeatMode != domain.RepeatOff {
		t.Errorf("expected empty snapshot, got %+v", state)
	}
}

func TestQueueService_Resume_ReturnsStored(t *testing.T) {
	repo := newInMemoryQueueRepo()
	svc := NewQueueService(repo)
	user := testUser()

	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:   []string{"x", "y"},
		CurrentIdx: 1,
		RepeatMode: "one",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	state, err := svc.Resume(context.Background(), user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.CurrentIdx != 1 || state.RepeatMode != domain.RepeatOne {
		t.Errorf("resumed state mismatch: %+v", state)
	}
}
