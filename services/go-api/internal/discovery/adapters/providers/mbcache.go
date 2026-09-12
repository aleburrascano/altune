package providers

import (
	"sync"
	"time"
)

const mbMemoTTL = 6 * time.Hour

type mbMemo[V any] struct {
	mu  sync.RWMutex
	ttl time.Duration
	m   map[string]mbMemoEntry[V]
}

type mbMemoEntry[V any] struct {
	val     V
	expires time.Time
}

func newMBMemo[V any](ttl time.Duration) *mbMemo[V] {
	return &mbMemo[V]{ttl: ttl, m: make(map[string]mbMemoEntry[V])}
}

func (c *mbMemo[V]) get(key string) (V, bool) {
	c.mu.RLock()
	e, ok := c.m[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(e.expires) {
		var zero V
		return zero, false
	}
	return e.val, true
}

func (c *mbMemo[V]) put(key string, v V) {
	c.mu.Lock()
	c.m[key] = mbMemoEntry[V]{val: v, expires: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}
