package discoverytest

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"sync"
)

type FakeEventStore struct {
	Mu     sync.Mutex
	Events []domain.InteractionEvent
	Err    error
}

func (f *FakeEventStore) Append(_ context.Context, e domain.InteractionEvent) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.Events = append(f.Events, e)
	return nil
}

func (f *FakeEventStore) Snapshot() []domain.InteractionEvent {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	return append([]domain.InteractionEvent(nil), f.Events...)
}

func (f *FakeEventStore) Recorded() []domain.InteractionEvent {
	return f.Snapshot()
}
