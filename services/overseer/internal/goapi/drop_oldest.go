package goapi

import (
	"math"
	"sync"
	"sync/atomic"
)

type dropOldestQueue[T any] struct {
	mu      sync.Mutex
	pending chan T
	drops   dropCounter
}

func (q *dropOldestQueue[T]) push(item T) {
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

func (q *dropOldestQueue[T]) drain() []T {
	q.mu.Lock()
	defer q.mu.Unlock()
	return drainChannel(q.pending)
}

func (q *dropOldestQueue[T]) dropped() int { return q.drops.count() }

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

type dropSource interface {
	Dropped() int
}

func TotalDropped(src any, evicted int) int {
	streamDrops := 0
	if ds, ok := src.(dropSource); ok {
		streamDrops = ds.Dropped()
	}
	if streamDrops > math.MaxInt-evicted {
		return math.MaxInt
	}
	return evicted + streamDrops
}
