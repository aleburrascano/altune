package memory

import (
	"altune/go-api/internal/platform/monitoring/internal/domain"
	"altune/go-api/internal/platform/monitoring/internal/ports"
	"altune/go-api/internal/shared/ids"
	"context"
	"sync"
	"time"
)

var _ ports.ClientEventStore = (*ClientEventRepo)(nil)

type eventKey struct {
	user    ids.UserId
	eventID string
}

type ClientEventRepo struct {
	mu         sync.Mutex
	rows       []domain.ClientEvent
	seen       map[eventKey]struct{}
	identities map[ids.UserId]struct{}
}

func NewClientEventRepo() *ClientEventRepo {
	return &ClientEventRepo{seen: map[eventKey]struct{}{}, identities: map[ids.UserId]struct{}{}}
}

func (r *ClientEventRepo) Append(_ context.Context, event domain.ClientEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if event.EventId != "" {
		key := eventKey{user: event.UserId, eventID: event.EventId}
		if _, dup := r.seen[key]; dup {
			return nil
		}
		r.seen[key] = struct{}{}
	}
	r.rows = append(r.rows, event)
	return nil
}

func (r *ClientEventRepo) Prune(_ context.Context, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := now.UTC().Add(-domain.ClientEventRetention)
	return r.deleteWhere(func(e domain.ClientEvent) bool { return e.OccurredAt.UTC().Before(cutoff) }), nil
}

func (r *ClientEventRepo) EraseRowsOfDeletedIdentities(context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.identities) == 0 {
		return 0, nil
	}
	system := ids.SystemUserId()
	return r.deleteWhere(func(e domain.ClientEvent) bool {
		_, alive := r.identities[e.UserId]
		return e.UserId != system && !alive
	}), nil
}

func (r *ClientEventRepo) RegisterIdentity(user ids.UserId) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.identities[user] = struct{}{}
}

func (r *ClientEventRepo) Count(user ids.UserId) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.rows {
		if e.UserId == user {
			n++
		}
	}
	return n
}

func (r *ClientEventRepo) deleteWhere(drop func(domain.ClientEvent) bool) int64 {
	kept := r.rows[:0]
	var removed int64
	for _, e := range r.rows {
		if drop(e) {
			removed++
			if e.EventId != "" {
				delete(r.seen, eventKey{user: e.UserId, eventID: e.EventId})
			}
			continue
		}
		kept = append(kept, e)
	}
	r.rows = kept
	return removed
}
