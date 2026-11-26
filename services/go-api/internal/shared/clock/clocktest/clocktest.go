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
	waiters map[int]waiter
	nextKey int
	arrived *sync.Cond
}

type waiter struct {
	wakeAt time.Time
	period time.Duration
	fire   func(now time.Time)
}

func NewManual(start time.Time) *Manual {
	return &Manual{now: start, waiters: map[int]waiter{}}
}

func (m *Manual) Sleeper() clock.Sleeper {
	return m.sleep
}

func (m *Manual) Timer() clock.TimerFunc {
	return m.newTimer
}

func (m *Manual) Ticker() clock.TickerFunc {
	return m.newTicker
}

func (m *Manual) BlockUntil(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for len(m.waiters) < n {
		m.arrivalsLocked().Wait()
	}
}

func (m *Manual) arrivalsLocked() *sync.Cond {
	if m.arrived == nil {
		m.arrived = sync.NewCond(&m.mu)
	}
	return m.arrived
}

func (m *Manual) Sleeping() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.waiters)
}

func (m *Manual) sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	wake := make(chan struct{})
	key := m.register(d, func(time.Time) { close(wake) })

	select {
	case <-wake:
		return nil
	case <-ctx.Done():
		m.cancel(key)
		return ctx.Err()
	}
}

func (m *Manual) newTimer(d time.Duration) (<-chan time.Time, func() bool) {
	fires := make(chan time.Time, 1)
	if d <= 0 {
		fires <- m.Now()
		return fires, func() bool { return false }
	}
	key := m.register(d, func(now time.Time) { fires <- now })
	return fires, func() bool { return m.cancel(key) }
}

func (m *Manual) newTicker(d time.Duration) (<-chan time.Time, func()) {
	if d <= 0 {
		panic("clocktest: non-positive interval for Ticker")
	}
	ticks := make(chan time.Time, 1)
	key := m.registerEvery(d, d, func(now time.Time) {
		select {
		case ticks <- now:
		default:
		}
	})
	return ticks, func() { m.cancel(key) }
}

func (m *Manual) register(d time.Duration, fire func(time.Time)) int {
	return m.registerEvery(d, 0, fire)
}

func (m *Manual) registerEvery(d, period time.Duration, fire func(time.Time)) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.waiters == nil {
		m.waiters = map[int]waiter{}
	}
	key := m.nextKey
	m.nextKey++
	m.waiters[key] = waiter{wakeAt: m.now.Add(d), period: period, fire: fire}
	m.arrivalsLocked().Broadcast()
	return key
}

func (m *Manual) cancel(key int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, pending := m.waiters[key]
	delete(m.waiters, key)
	return pending
}

func (m *Manual) wakeDueLocked() {
	for fired := true; fired; {
		fired = false
		for key, w := range m.waiters {
			if w.wakeAt.After(m.now) {
				continue
			}
			fired = true
			if w.period > 0 {
				w.wakeAt = w.wakeAt.Add(w.period)
				m.waiters[key] = w
			} else {
				delete(m.waiters, key)
			}
			w.fire(m.now)
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
