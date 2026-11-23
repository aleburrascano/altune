package ttlmemo

import (
	"sync"
	"time"
)

type Memo[K comparable, V any] struct {
	ttl        time.Duration
	maxEntries int
	now        func() time.Time
	mu         sync.Mutex
	entries    map[K]entry[V]
}

type entry[V any] struct {
	value     V
	expiresAt time.Time
}

func New[K comparable, V any](ttl time.Duration, maxEntries int, now func() time.Time) *Memo[K, V] {
	return &Memo[K, V]{ttl: ttl, maxEntries: maxEntries, now: now, entries: map[K]entry[V]{}}
}

func (m *Memo[K, V]) Get(key K) (V, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, memoized := m.entries[key]
	if !memoized || m.now().After(e.expiresAt) {
		var zero V
		return zero, false
	}
	return e.value, true
}

func (m *Memo[K, V]) Put(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.entries) >= m.maxEntries {
		m.dropExpired()
	}
	if len(m.entries) < m.maxEntries {
		m.entries[key] = entry[V]{value: value, expiresAt: m.now().Add(m.ttl)}
	}
}

func (m *Memo[K, V]) dropExpired() {
	now := m.now()
	for key, e := range m.entries {
		if now.After(e.expiresAt) {
			delete(m.entries, key)
		}
	}
}
