package memo

import (
	"container/list"
	"sync"
	"time"
)

type Memo[V any] struct {
	mu        sync.Mutex
	ttl       time.Duration
	max       int
	now       func() time.Time
	m         map[string]*list.Element
	order     *list.List
	lastSweep time.Time
}

type entry[V any] struct {
	key     string
	val     V
	expires time.Time
}

func New[V any](ttl time.Duration, maxEntries int, now func() time.Time) *Memo[V] {
	return &Memo[V]{
		ttl:       ttl,
		max:       max(maxEntries, 1),
		now:       now,
		m:         make(map[string]*list.Element),
		order:     list.New(),
		lastSweep: now(),
	}
}

func (c *Memo[V]) Get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var zero V
	el, ok := c.m[key]
	if !ok {
		return zero, false
	}
	e := el.Value.(*entry[V])
	if c.now().After(e.expires) {
		c.remove(el)
		return zero, false
	}
	c.order.MoveToFront(el)
	return e.val, true
}

func (c *Memo[V]) Put(key string, v V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if el, ok := c.m[key]; ok {
		e := el.Value.(*entry[V])
		e.val = v
		e.expires = now.Add(c.ttl)
		c.order.MoveToFront(el)
		return
	}
	c.makeRoom(now)
	c.m[key] = c.order.PushFront(&entry[V]{key: key, val: v, expires: now.Add(c.ttl)})
}

func (c *Memo[V]) makeRoom(now time.Time) {
	if len(c.m) >= c.max && now.Sub(c.lastSweep) >= c.ttl {
		c.sweepExpired(now)
	}
	for len(c.m) >= c.max {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		c.remove(oldest)
	}
}

func (c *Memo[V]) sweepExpired(now time.Time) {
	c.lastSweep = now
	for el := c.order.Back(); el != nil; {
		prev := el.Prev()
		if now.After(el.Value.(*entry[V]).expires) {
			c.remove(el)
		}
		el = prev
	}
}

func (c *Memo[V]) remove(el *list.Element) {
	c.order.Remove(el)
	delete(c.m, el.Value.(*entry[V]).key)
}
