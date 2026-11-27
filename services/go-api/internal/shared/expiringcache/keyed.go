package expiringcache

import (
	"cmp"
	"slices"
	"sync"
	"time"
)

type bucket interface {
	TokensAt(time.Time) float64
	Burst() int
}

type Keyed[K comparable, V bucket] struct {
	mu        sync.Mutex
	ttl       time.Duration
	max       int
	now       func() time.Time
	active    map[K]V
	cooling   map[K]V
	rotatedAt time.Time
}

func NewKeyed[K comparable, V bucket](idleAfter time.Duration, maxKeys int, now func() time.Time) *Keyed[K, V] {
	return &Keyed[K, V]{
		ttl:     max(idleAfter, time.Nanosecond),
		max:     max(maxKeys, 1),
		now:     now,
		active:  make(map[K]V),
		cooling: make(map[K]V),
	}
}

func (k *Keyed[K, V]) Get(key K, create func() V) V {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.now()
	k.rotate(now)
	if v, ok := k.active[key]; ok {
		return v
	}
	if v, ok := k.cooling[key]; ok {
		delete(k.cooling, key)
		k.active[key] = v
		return v
	}
	k.makeRoom(now)
	v := create()
	k.active[key] = v
	return v
}

func (k *Keyed[K, V]) Len() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.active) + len(k.cooling)
}

func (k *Keyed[K, V]) rotate(now time.Time) {
	if now.Sub(k.rotatedAt) < k.ttl {
		return
	}
	k.rotatedAt = now
	k.cooling = k.active
	k.active = make(map[K]V)
}

func (k *Keyed[K, V]) size() int { return len(k.active) + len(k.cooling) }

func (k *Keyed[K, V]) makeRoom(now time.Time) {
	if k.size() < k.max {
		return
	}
	k.pruneRefilled(now)
	k.evictFullest(now, k.max-max(1, k.max/10))
}

func (k *Keyed[K, V]) pruneRefilled(now time.Time) {
	for _, m := range []map[K]V{k.active, k.cooling} {
		for key, v := range m {
			if v.TokensAt(now) >= float64(v.Burst()) {
				delete(m, key)
			}
		}
	}
}

func (k *Keyed[K, V]) evictFullest(now time.Time, lowWater int) {
	type scored struct {
		key    K
		tokens float64
	}
	all := make([]scored, 0, k.size())
	for _, m := range []map[K]V{k.active, k.cooling} {
		for key, v := range m {
			all = append(all, scored{key, v.TokensAt(now)})
		}
	}
	slices.SortFunc(all, func(a, b scored) int { return cmp.Compare(b.tokens, a.tokens) })
	for _, s := range all {
		if k.size() <= lowWater {
			return
		}
		delete(k.active, s.key)
		delete(k.cooling, s.key)
	}
}
