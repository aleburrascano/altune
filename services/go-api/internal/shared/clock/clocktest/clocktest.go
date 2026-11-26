package clocktest

import (
	"altune/go-api/internal/shared/clock"
	"context"
	"sync"
	"time"
)

type Manual struct {
	mu      sync.Mutex
	now     time.Time
	sleeps  map[int]sleep
	nextKey int
}

type sleep struct {
	wakeAt time.Time
	wake   chan struct{}
}

func NewManual(start time.Time) *Manual {
	return &Manual{now: start, sleeps: map[int]sleep{}}
}

func (m *Manual) Sleeper() clock.Sleeper {
	return m.sleep
}

func (m *Manual) Sleeping() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sleeps)
}

func (m *Manual) sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	m.mu.Lock()
	if m.sleeps == nil {
		m.sleeps = map[int]sleep{}
	}
	key := m.nextKey
	m.nextKey++
	wake := make(chan struct{})
	m.sleeps[key] = sleep{wakeAt: m.now.Add(d), wake: wake}
	m.mu.Unlock()

	select {
	case <-wake:
		return nil
	case <-ctx.Done():
		m.mu.Lock()
		delete(m.sleeps, key)
		m.mu.Unlock()
		return ctx.Err()
	}
}

func (m *Manual) wakeDueLocked() {
	for key, s := range m.sleeps {
		if !s.wakeAt.After(m.now) {
			delete(m.sleeps, key)
			close(s.wake)
		}
	}
}

func (m *Manual) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

func (m *Manual) Func() clock.Func {
	return m.Now
}

func (m *Manual) Advance(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = m.now.Add(d)
	m.wakeDueLocked()
}

func (m *Manual) Set(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = t
	m.wakeDueLocked()
}
