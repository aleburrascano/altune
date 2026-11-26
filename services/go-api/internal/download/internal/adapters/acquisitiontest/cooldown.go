package acquisitiontest

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var _ ports.CooldownStore = (*CooldownStore)(nil)

type cooldownKey struct {
	track ids.TrackId
	kind  ports.CooldownKind
}

type CooldownStore struct {
	mu         sync.Mutex
	now        clock.Func
	admittedAt map[cooldownKey]time.Time
	reserveErr error
	releaseErr error
	releases   int
}

var ErrCooldownClockRequired = errors.New("the cooldown store needs a clock")

func NewCooldownStore(now clock.Func) (*CooldownStore, error) {
	if now == nil {
		return nil, fmt.Errorf("NewCooldownStore: %w", ErrCooldownClockRequired)
	}
	return &CooldownStore{now: now, admittedAt: map[cooldownKey]time.Time{}}, nil
}

func MustNewCooldownStore(now clock.Func) *CooldownStore {
	s, err := NewCooldownStore(now)
	if err != nil {
		panic(err)
	}
	return s
}

func (s *CooldownStore) FailReserve(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reserveErr = err
}

func (s *CooldownStore) FailRelease(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseErr = err
}

func (s *CooldownStore) Releases() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.releases
}

func (s *CooldownStore) Reserve(_ context.Context, trackID ids.TrackId, kind ports.CooldownKind, cooldown time.Duration) (time.Time, bool, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reserveErr; err != nil {
		return time.Time{}, false, 0, err
	}
	now, key := s.now(), cooldownKey{track: trackID, kind: kind}
	if last, held := s.admittedAt[key]; held && now.Sub(last) < cooldown {
		return time.Time{}, false, cooldown - now.Sub(last), nil
	}
	s.admittedAt[key] = now
	return now, true, 0, nil
}

func (s *CooldownStore) Release(ctx context.Context, trackID ids.TrackId, kind ports.CooldownKind, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releases++
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.releaseErr; err != nil {
		return err
	}
	key := cooldownKey{track: trackID, kind: kind}
	if last, held := s.admittedAt[key]; held && last.Equal(at) {
		delete(s.admittedAt, key)
	}
	return nil
}
