package clocktest

import (
	"altune/go-api/internal/shared/clock"
	"sync"
	"time"
)

type Manual struct {
	mu  sync.Mutex
	now time.Time
}

func NewManual(start time.Time) *Manual {
	return &Manual{now: start}
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
}

func (m *Manual) Set(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = t
}
