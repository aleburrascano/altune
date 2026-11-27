package expiringcache

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"
)

type Stats struct {
	Hits, Misses uint64
}

type Memo[K comparable, V any] struct {
	mu        sync.Mutex
	ttl       time.Duration
	max       int
	now       func() time.Time
	entries   map[K]*list.Element
	order     *list.List
	lastSweep time.Time
	hits      atomic.Uint64
	misses    atomic.Uint64
}

type entry[K comparable, V any] struct {
	key     K
	value   V
	expires time.Time
}

func New[K comparable, V any](ttl time.Duration, maxEntries int, now func() time.Time) *Memo[K, V] {
	return &Memo[K, V]{
		ttl:       ttl,
		max:       max(maxEntries, 1),
		now:       now,
		entries:   make(map[K]*list.Element),
		order:     list.New(),
		lastSweep: now(),
	}
}

func (m *Memo[K, V]) Get(key K) (V, bool) {
	value, ok := m.Peek(key)
	if ok {
		m.hits.Add(1)
	} else {
		m.misses.Add(1)
	}
	return value, ok
}

func (m *Memo[K, V]) Stats() Stats {
	return Stats{Hits: m.hits.Load(), Misses: m.misses.Load()}
}

func (m *Memo[K, V]) Peek(key K) (V, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var zero V
	el, ok := m.entries[key]
	if !ok {
		return zero, false
	}
	e := el.Value.(*entry[K, V])
	if m.now().After(e.expires) {
		m.remove(el)
		return zero, false
	}
	m.order.MoveToFront(el)
	return e.value, true
}

func (m *Memo[K, V]) Put(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if el, ok := m.entries[key]; ok {
		e := el.Value.(*entry[K, V])
		e.value = value
		e.expires = now.Add(m.ttl)
		m.order.MoveToFront(el)
		return
	}
	m.makeRoom(now)
	m.entries[key] = m.order.PushFront(&entry[K, V]{key: key, value: value, expires: now.Add(m.ttl)})
}

func (m *Memo[K, V]) makeRoom(now time.Time) {
	if len(m.entries) >= m.max && now.Sub(m.lastSweep) >= m.ttl {
		m.sweepExpired(now)
	}
	for len(m.entries) >= m.max {
		oldest := m.order.Back()
		if oldest == nil {
			break
		}
		m.remove(oldest)
	}
}

func (m *Memo[K, V]) sweepExpired(now time.Time) {
	m.lastSweep = now
	for el := m.order.Back(); el != nil; {
		prev := el.Prev()
		if now.After(el.Value.(*entry[K, V]).expires) {
			m.remove(el)
		}
		el = prev
	}
}

func (m *Memo[K, V]) remove(el *list.Element) {
	m.order.Remove(el)
	delete(m.entries, el.Value.(*entry[K, V]).key)
}
