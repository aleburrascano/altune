package logging

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

const logRingCapacity = 1000

const logRetentionWindow = 30 * time.Minute

const subscriberChanSize = 64

const MaxSubscribers = 16

var ErrTooManySubscribers = errors.New("logging: too many log stream subscribers")

type CapturedRecord struct {
	Time    time.Time         `json:"time"`
	Level   string            `json:"level"`
	Message string            `json:"msg"`
	Attrs   map[string]string `json:"attrs,omitempty"`
}

type RingBuffer struct {
	mu         sync.Mutex
	buf        []CapturedRecord
	capturedAt []time.Time
	head       int
	count      int
	retention  time.Duration
	now        func() time.Time
	subs       map[int]chan CapturedRecord
	nextSub    int
	maxSubs    int
	dropped    atomic.Uint64
}

func (rb *RingBuffer) Dropped() uint64 { return rb.dropped.Load() }

func NewRingBuffer(capacity int) *RingBuffer {
	return newRingBufferWithClock(capacity, time.Now)
}

func newRingBufferWithClock(capacity int, now func() time.Time) *RingBuffer {
	if capacity < 1 {
		capacity = 1
	}
	return &RingBuffer{
		buf:        make([]CapturedRecord, capacity),
		capturedAt: make([]time.Time, capacity),
		retention:  logRetentionWindow,
		now:        now,
		subs:       make(map[int]chan CapturedRecord),
		maxSubs:    MaxSubscribers,
	}
}

func (rb *RingBuffer) append(rec CapturedRecord) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.evictExpiredLocked()
	rb.buf[rb.head] = rec
	rb.capturedAt[rb.head] = rb.now()
	rb.head = (rb.head + 1) % len(rb.buf)
	if rb.count < len(rb.buf) {
		rb.count++
	}
	rb.fanOutDroppingWhenSubscriberIsFull(rec)
}

func (rb *RingBuffer) evictExpiredLocked() {
	cutoff := rb.now().Add(-rb.retention)
	for rb.count > 0 {
		oldest := rb.oldestLocked()
		if !rb.capturedAt[oldest].Before(cutoff) {
			return
		}
		rb.buf[oldest] = CapturedRecord{}
		rb.count--
	}
}

func (rb *RingBuffer) oldestLocked() int {
	return (rb.head - rb.count + len(rb.buf)) % len(rb.buf)
}

func (rb *RingBuffer) fanOutDroppingWhenSubscriberIsFull(rec CapturedRecord) {
	for _, ch := range rb.subs {
		select {
		case ch <- rec:
		default:
			rb.dropped.Add(1)
		}
	}
}

func (rb *RingBuffer) Snapshot() []CapturedRecord {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.evictExpiredLocked()
	out := make([]CapturedRecord, 0, rb.count)
	start := rb.oldestLocked()
	for i := 0; i < rb.count; i++ {
		out = append(out, rb.buf[(start+i)%len(rb.buf)])
	}
	return out
}

func (rb *RingBuffer) Subscribe() (<-chan CapturedRecord, func(), error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if len(rb.subs) >= rb.maxSubs {
		return nil, nil, ErrTooManySubscribers
	}
	id := rb.nextSub
	rb.nextSub++
	ch := make(chan CapturedRecord, subscriberChanSize)
	rb.subs[id] = ch
	return ch, func() { rb.unsubscribe(id) }, nil
}

func (rb *RingBuffer) unsubscribe(id int) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if c, ok := rb.subs[id]; ok {
		delete(rb.subs, id)
		close(c)
	}
}
