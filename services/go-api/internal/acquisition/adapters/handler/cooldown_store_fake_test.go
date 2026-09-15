package handler

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"sync"
	"time"

	catdomain "altune/go-api/internal/catalog/domain"
)

// memCooldownStore is an in-memory ports.CooldownStore for handler tests.
type memCooldownStore struct {
	mu     sync.Mutex
	lastAt map[string]time.Time
}

func newMemCooldownStore() *memCooldownStore {
	return &memCooldownStore{lastAt: make(map[string]time.Time)}
}

func (s *memCooldownStore) Reserve(_ context.Context, trackID catdomain.TrackId, kind ports.CooldownKind, cooldown time.Duration) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now, key := time.Now(), string(kind)+"/"+trackID.String()
	if last, ok := s.lastAt[key]; ok && now.Sub(last) < cooldown {
		return time.Time{}, false, nil
	}
	s.lastAt[key] = now
	return now, true, nil
}

func (s *memCooldownStore) Release(_ context.Context, trackID catdomain.TrackId, kind ports.CooldownKind, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := string(kind) + "/" + trackID.String()
	if last, ok := s.lastAt[key]; ok && last.Equal(at) {
		delete(s.lastAt, key)
	}
	return nil
}
