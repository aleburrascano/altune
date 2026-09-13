package logging

import (
	"sync"
	"time"
)

const logRingCapacity = 1000

const subscriberChanSize = 64

type CapturedRecord struct {
	Time    time.Time         `json:"time"`
	Level   string            `json:"level"`
	Message string            `json:"msg"`
	Attrs   map[string]string `json:"attrs,omitempty"`
}

type RingBuffer struct {
	mu      sync.Mutex
	buf     []CapturedRecord
	head    int
	count   int
	subs    map[int]chan CapturedRecord
	nextSub int
}

func NewRingBuffer(capacity int) *RingBuffer {
	if capacity < 1 {
		capacity = 1
	}
	return &RingBuffer{
		buf:  make([]CapturedRecord, capacity),
		subs: make(map[int]chan CapturedRecord),
	}
}

func (rb *RingBuffer) append(rec CapturedRecord) {
	rb.mu.Lock()
	rb.buf[rb.head] = rec
	rb.head = (rb.head + 1) % len(rb.buf)
	if rb.count < len(rb.buf) {
		rb.count++
	}
	rb.fanOutDroppingWhenSubscriberIsFull(rec)
	rb.mu.Unlock()
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
	out := make([]CapturedRecord, 0, rb.count)
	start := (rb.head - rb.count + len(rb.buf)) % len(rb.buf)
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
