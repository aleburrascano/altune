package goapihttp

import (
	"math"
	"sync"
	"sync/atomic"
)

type DropOldestQueue[T any] struct {
	mu      sync.Mutex
	pending chan T
	drops   dropCounter
}

func NewDropOldestQueue[T any](size int) *DropOldestQueue[T] {
	return &DropOldestQueue[T]{pending: make(chan T, size)}
}

func (q *DropOldestQueue[T]) Push(item T) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for {
		select {
		case q.pending <- item:
			return
		default:
		}
		select {
		case <-q.pending:
			q.drops.record()
		default:
		}
	}
}

func (q *DropOldestQueue[T]) Drain() []T {
	q.mu.Lock()
	defer q.mu.Unlock()
	return drainChannel(q.pending)
}

func (q *DropOldestQueue[T]) Dropped() int { return q.drops.count() }

func (q *DropOldestQueue[T]) Len() int { return len(q.pending) }

type pendingDrainer[T any] interface {
	drainPending() []T
}

func DrainPending[T any](src any, pending <-chan T) []T {
	if drainer, ok := src.(pendingDrainer[T]); ok {
		return drainer.drainPending()
	}
	return drainChannel(pending)
}

func drainChannel[T any](pending <-chan T) []T {
	var drained []T
	for {
		select {
		case item, ok := <-pending:
			if !ok {
				return drained
			}
			drained = append(drained, item)
		default:
			return drained
		}
	}
}

type dropCounter struct {
	total atomic.Int64
}

func (d *dropCounter) record() {
	for {
		current := d.total.Load()
		if current >= math.MaxInt {
			return
		}
		if d.total.CompareAndSwap(current, current+1) {
			return
		}
	}
}

func (d *dropCounter) count() int { return int(d.total.Load()) }
