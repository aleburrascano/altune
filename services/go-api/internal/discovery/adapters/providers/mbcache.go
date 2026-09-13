package providers

import (
	"container/list"
	"sync"
	"time"
)

const mbMemoTTL = 6 * time.Hour

// mbMemoMaxEntries caps each memo. Keys are normalized artist names taken
// from user search results, so without a cap every distinct name ever
// searched would stay in memory for the life of the process.
const mbMemoMaxEntries = 2048

// mbMemo is a TTL + LRU memo. Expired entries are deleted when read, and a
// put into a full memo first sweeps expired entries (at most once per TTL)
// and then evicts the least recently used entry if still full.
type mbMemo[V any] struct {
	mu        sync.Mutex
	ttl       time.Duration
	max       int
	m         map[string]*list.Element
	order     *list.List // front = most recently used
	lastSweep time.Time
}

type mbMemoEntry[V any] struct {
	key     string
	val     V
	expires time.Time
}

func newMBMemo[V any](ttl time.Duration) *mbMemo[V] {
	return newMBMemoCap[V](ttl, mbMemoMaxEntries)
}

func newMBMemoCap[V any](ttl time.Duration, maxEntries int) *mbMemo[V] {
	if maxEntries < 1 {
		maxEntries = 1
	}
	return &mbMemo[V]{
		ttl:       ttl,
		max:       maxEntries,
		m:         make(map[string]*list.Element),
		order:     list.New(),
		lastSweep: time.Now(),
	}
}

func (c *mbMemo[V]) get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var zero V
	el, ok := c.m[key]
	if !ok {
		return zero, false
	}
	e := el.Value.(*mbMemoEntry[V])
	if time.Now().After(e.expires) {
		c.remove(el)
		return zero, false
	}
	c.order.MoveToFront(el)
	return e.val, true
}

func (c *mbMemo[V]) put(key string, v V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if el, ok := c.m[key]; ok {
		e := el.Value.(*mbMemoEntry[V])
		e.val = v
		e.expires = now.Add(c.ttl)
		c.order.MoveToFront(el)
		return
	}
	if len(c.m) >= c.max && now.Sub(c.lastSweep) >= c.ttl {
		c.sweepExpired(now)
	}
	for len(c.m) >= c.max {
		c.remove(c.order.Back())
	}
	c.m[key] = c.order.PushFront(&mbMemoEntry[V]{key: key, val: v, expires: now.Add(c.ttl)})
}

func (c *mbMemo[V]) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.m)
}

// sweepExpired must be called with c.mu held.
func (c *mbMemo[V]) sweepExpired(now time.Time) {
	c.lastSweep = now
	for el := c.order.Back(); el != nil; {
		prev := el.Prev()
		if now.After(el.Value.(*mbMemoEntry[V]).expires) {
			c.remove(el)
		}
		el = prev
	}
}

// remove must be called with c.mu held.
func (c *mbMemo[V]) remove(el *list.Element) {
	c.order.Remove(el)
	delete(c.m, el.Value.(*mbMemoEntry[V]).key)
}
