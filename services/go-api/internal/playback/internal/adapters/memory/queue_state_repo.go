package memory

import (
	ports "altune/go-api/internal/playback/internal"
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/shared/ids"
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

var _ ports.QueueStateRepository = (*QueueStateRepo)(nil)

type QueueStateRepo struct {
	mu     sync.Mutex
	states map[uuid.UUID]*domain.QueueState
}

func NewQueueStateRepo() *QueueStateRepo {
	return &QueueStateRepo{states: map[uuid.UUID]*domain.QueueState{}}
}

func (r *QueueStateRepo) Seed(state *domain.QueueState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := *state
	r.states[state.UserId.UUID()] = &copied
}

func (r *QueueStateRepo) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.states)
}

func (r *QueueStateRepo) UserIds() []ids.UserId {
	r.mu.Lock()
	defer r.mu.Unlock()
	owners := make([]ids.UserId, 0, len(r.states))
	for id := range r.states {
		owners = append(owners, ids.NewUserId(id))
	}
	return owners
}

func (r *QueueStateRepo) Upsert(_ context.Context, state *domain.QueueState) error {
	if err := state.Validate(); err != nil {
		return fmt.Errorf("refuse invalid queue state: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if stored, ok := r.states[state.UserId.UUID()]; ok && stored.UpdatedAt.After(state.UpdatedAt) {
		return domain.ErrStaleQueueWrite
	}
	copied := *state
	r.states[state.UserId.UUID()] = &copied
	return nil
}

func (r *QueueStateRepo) UpdatePosition(_ context.Context, position *domain.QueuePosition) error {
	if err := position.Validate(); err != nil {
		return fmt.Errorf("refuse invalid queue position: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := r.states[position.UserId.UUID()]
	if stored == nil || position.CurrentIdx >= len(stored.TrackIds) || stored.TrackIds[position.CurrentIdx] != position.CurrentTrackId {
		return domain.ErrQueuePositionMismatch
	}
	if stored.UpdatedAt.After(position.UpdatedAt) {
		return domain.ErrStaleQueueWrite
	}
	next := *stored
	next.CurrentIdx = position.CurrentIdx
	next.PositionMs = position.PositionMs
	next.UpdatedAt = position.UpdatedAt
	r.states[position.UserId.UUID()] = &next
	return nil
}

func (r *QueueStateRepo) GetForUser(_ context.Context, userId ids.UserId) (*domain.QueueState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.states[userId.UUID()]
	if !ok {
		return nil, domain.ErrQueueStateNotFound
	}
	copied := *state
	return &copied, nil
}

func (r *QueueStateRepo) DeleteForUser(_ context.Context, userId ids.UserId) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.states, userId.UUID())
	return nil
}
