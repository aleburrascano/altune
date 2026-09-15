package logging

import (
	"sync"
	"time"
)

const logRingCapacity = 1000

// logRetentionWindow bounds how long a captured record stays readable through
// the admin logs feed, independent of the capacity bound. Records can carry
// user queries and diagnostic error text, so a quiet ring must not keep them
// indefinitely. Mirrors requeststore's retention window.
const logRetentionWindow = 30 * time.Minute

const subscriberChanSize = 64

type CapturedRecord struct {
	Time    time.Time         `json:"time"`
	Level   string            `json:"level"`
	Message string            `json:"msg"`
	Attrs   map[string]string `json:"attrs,omitempty"`
}

type RingBuffer struct {
	mu  sync.Mutex
	buf []CapturedRecord
	// capturedAt stamps when each slot was appended, by the ring's own clock,
	// so age eviction never trusts a caller-supplied record time.
	capturedAt []time.Time
	head       int
	count      int
	retention  time.Duration
	now        func() time.Time
	subs       map[int]chan CapturedRecord
	nextSub    int
}

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

// evictExpiredLocked drops records older than the retention window, oldest
// first, zeroing each slot so the evicted contents are released.
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

func (rb *RingBuffer) Subscribe() (<-chan CapturedRecord, func()) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	id := rb.nextSub
	rb.nextSub++
	ch := make(chan CapturedRecord, subscriberChanSize)
	rb.subs[id] = ch
	return ch, func() {
		rb.mu.Lock()
		defer rb.mu.Unlock()
		if c, ok := rb.subs[id]; ok {
			delete(rb.subs, id)
			close(c)
		}
	}
}
