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

// MaxSubscribers bounds concurrent live subscribers to the log stream. The
// stream is operator-only, so a handful of open consoles is the real load; the
// ceiling exists so a leaked token or reconnect storm cannot grow the
// subscriber set (one goroutine and buffered channel each) without limit.
const MaxSubscribers = 16

// ErrTooManySubscribers is returned by Subscribe once MaxSubscribers are live.
var ErrTooManySubscribers = errors.New("logging: too many log stream subscribers")

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
	maxSubs    int
	// dropped is read by callers that do not hold mu, so it is atomic rather
	// than mu-guarded like the ring itself.
	dropped atomic.Uint64
}

// Dropped reports how many records a full subscriber channel discarded since
// startup. A rising count means the live log stream an operator is watching
// has gaps the stream itself cannot show.
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

// A drop is counted and never logged, unlike the identical case in
// events.InProcessBus: this runs inside the slog handler's own append path
// while holding mu, so a log line here would re-enter append and deadlock, and
// would add to the very burst that filled the channel. Dropped is the signal.
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

// Subscribe opens a live record subscription. It returns
// ErrTooManySubscribers, and no channel, once MaxSubscribers subscriptions are
// open; the returned cancel func must be called to release the slot.
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
