package core

import (
	"math"
	"sync"
)

type Store interface {
	Add(s Signal)
	Snapshot() []Signal
	Len() int
	Cap() int
	Dropped() int
}

type RingStore struct {
	mu      sync.RWMutex
	buf     []Signal
	next    int
	count   int
	dropped int
}

func NewRingStore(capacity int) *RingStore {
	if capacity < 1 {
		capacity = 1
	}
	return &RingStore{buf: make([]Signal, capacity)}
}

func (r *RingStore) Add(s Signal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.next] = s
	r.next = (r.next + 1) % len(r.buf)
	if r.count < len(r.buf) {
		r.count++
		return
	}
	if r.dropped < math.MaxInt {
		r.dropped++
	}
}

func (r *RingStore) Snapshot() []Signal {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Signal, 0, r.count)
	start := r.oldestIndex()
	for i := 0; i < r.count; i++ {
		out = append(out, r.buf[(start+i)%len(r.buf)])
	}
	return out
}

func (r *RingStore) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.count
}

func (r *RingStore) Cap() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.buf)
}

func (r *RingStore) Dropped() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dropped
}

func (r *RingStore) Restore(saved []Signal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	evicted := max(0, len(saved)-len(r.buf))
	clear(r.buf)
	r.count, r.dropped = 0, 0
	for i, s := range saved {
		if i >= evicted {
			r.buf[r.count] = s
			r.count++
		}
	}
	r.next = r.count % len(r.buf)
}

func (r *RingStore) Cursor() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.count + r.dropped
}

func (r *RingStore) AddedSince(mark int) (fresh []Signal, added int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	added = r.count + r.dropped
	unseen := min(max(0, added-mark), r.count)
	fresh = make([]Signal, 0, unseen)
	start := r.oldestIndex() + r.count - unseen
	for i := range unseen {
		fresh = append(fresh, r.buf[(start+i)%len(r.buf)])
	}
	return fresh, added
}

func (r *RingStore) oldestIndex() int {
	if r.count < len(r.buf) {
		return 0
	}
	return r.next
}
