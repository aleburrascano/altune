package logging

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// logRingCapacity bounds the in-memory log tail. Sized for a single-operator
// console on a memory-constrained box, not durable retention.
const logRingCapacity = 1000

// subscriberChanSize buffers a live-tail subscriber before drops kick in.
const subscriberChanSize = 64

// ringCaptureFloor is the lowest level the ring retains, independent of stdout's
// level. DEBUG so the operator console keeps the rich provider/breaker lines even
// when production stdout runs at INFO.
const ringCaptureFloor = slog.LevelDebug

// CapturedRecord is a flattened copy of a log line retained for the Mission
// Control logs panel. The originating slog.Record is never retained — its
// Attrs are unsafe to read after Handle returns.
type CapturedRecord struct {
	Time    time.Time         `json:"time"`
	Level   string            `json:"level"`
	Message string            `json:"msg"`
	Attrs   map[string]string `json:"attrs,omitempty"`
}

// RingBuffer is a fixed-capacity, concurrency-safe ring of recent log records
// plus a lossy live-tail fan-out. Lossy by design (like the SSE event bus): a
// slow subscriber drops records rather than blocking the logging hot path.
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
	for _, ch := range rb.subs {
		select {
		case ch <- rec:
		default: // slow subscriber — drop rather than block the logger
		}
	}
	rb.mu.Unlock()
}

// Snapshot returns the retained records, oldest first.
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

// Subscribe returns a channel of newly appended records and a cancel func that
// unsubscribes and closes it. The caller must drain the channel; a full channel
// drops records.
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

// ringHandler tees every record to an inner handler (stdout) and captures a
// flattened copy into the shared ring. The ring pointer is shared across all
// WithAttrs/WithGroup-derived handlers so per-request child loggers feed the
// same buffer.
type ringHandler struct {
	inner slog.Handler
	ring  *RingBuffer
	attrs []slog.Attr
}

func newRingHandler(inner slog.Handler, ring *RingBuffer) *ringHandler {
	return &ringHandler{inner: inner, ring: ring}
}

// Enabled returns true for anything at or above the ring's capture floor (DEBUG)
// OR anything stdout wants — so the operator console retains the rich DEBUG
// provider/breaker lines even when stdout runs at INFO. Whether a record is also
// printed to stdout is decided per-record in Handle against the inner handler.
func (h *ringHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= ringCaptureFloor || h.inner.Enabled(ctx, level)
}

func (h *ringHandler) Handle(ctx context.Context, r slog.Record) error {
	// Build the captured copy outside any handler lock, then append under the
	// ring's own short-held mutex — never the inner handler's formatting lock.
	attrs := make(map[string]string, r.NumAttrs()+len(h.attrs))
	for _, a := range h.attrs {
		flattenAttr(attrs, "", a)
	}
	r.Attrs(func(a slog.Attr) bool {
		flattenAttr(attrs, "", a)
		return true
	})
	h.ring.append(CapturedRecord{
		Time:    r.Time,
		Level:   r.Level.String(),
		Message: r.Message,
		Attrs:   attrs,
	})
	// Forward to stdout only when stdout's own level wants this record — so DEBUG
	// captured for the console below the stdout level isn't also printed.
	if h.inner.Enabled(ctx, r.Level) {
		return h.inner.Handle(ctx, r)
	}
	return nil
}

func (h *ringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ringHandler{
		inner: h.inner.WithAttrs(attrs),
		ring:  h.ring, // shared pointer survives derivation
		attrs: append(append([]slog.Attr{}, h.attrs...), attrs...),
	}
}

func (h *ringHandler) WithGroup(name string) slog.Handler {
	return &ringHandler{
		inner: h.inner.WithGroup(name),
		ring:  h.ring,
		attrs: h.attrs,
	}
}

func flattenAttr(dst map[string]string, prefix string, a slog.Attr) {
	val := a.Value.Resolve()
	key := a.Key
	if prefix != "" {
		key = prefix + "." + key
	}
	if val.Kind() == slog.KindGroup {
		for _, ga := range val.Group() {
			flattenAttr(dst, key, ga)
		}
		return
	}
	dst[key] = val.String()
}
