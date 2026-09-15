package core

import "sync"

// Store is the bounded persistence contract every bucket writes through. The
// "bounded storage, always" invariant lives here: an implementation must never
// grow without limit. A persistent (Postgres-backed) implementation slots in
// later without any bucket changing, because buckets depend on this interface
// and never on a concrete store.
type Store interface {
	// Add records one signal, evicting the oldest if the bound is reached.
	Add(s Signal)
	// Snapshot returns the retained signals, oldest first, as a copy safe to
	// read without holding any lock.
	Snapshot() []Signal
	// Len is the number of retained signals; never exceeds Cap.
	Len() int
	// Cap is the fixed upper bound on retained signals.
	Cap() int
}

// RingStore is an in-memory, fixed-capacity ring buffer. Once full, each Add
// overwrites the oldest entry, so memory is bounded by construction. It is safe
// for concurrent use: a bucket's collect loop writes while an HTTP render reads.
type RingStore struct {
	mu    sync.RWMutex
	buf   []Signal
	next  int
	count int
}

// NewRingStore returns a ring bounded to capacity. A capacity below 1 is
// clamped to 1 so the store can never be unbounded or panic on Add.
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

// oldestIndex is the buffer position of the oldest retained signal. Callers must
// hold at least the read lock.
func (r *RingStore) oldestIndex() int {
	if r.count < len(r.buf) {
		return 0
	}
	return r.next
}
