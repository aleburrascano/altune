package clientevents

import (
	"altune/go-api/internal/platform/monitoring/internal/core/domain"
	"altune/go-api/internal/platform/monitoring/internal/core/ports"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"fmt"
)

var ErrCollaboratorRequired = errors.New("the client event recorder needs its store and clock")

type Recorder struct {
	store ports.ClientEventStore
	now   clock.Func
}

func NewRecorder(store ports.ClientEventStore, now clock.Func) (*Recorder, error) {
	if store == nil || now == nil {
		return nil, ErrCollaboratorRequired
	}
	return &Recorder{store: store, now: now}, nil
}

func (r *Recorder) Record(ctx context.Context, event domain.ClientEvent) error {
	if err := event.Validate(); err != nil {
		return fmt.Errorf("record client event: %w", err)
	}
	event.OccurredAt = r.now().UTC()
	if err := r.store.Append(ctx, event); err != nil {
		return fmt.Errorf("record client event: %w", err)
	}
	return nil
}
