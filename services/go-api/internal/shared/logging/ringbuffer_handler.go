package logging

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const logRingCapacity = 1000

const subscriberChanSize = 64

const ringCaptureFloor = slog.LevelDebug

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

type ringHandler struct {
	inner slog.Handler
	ring  *RingBuffer
	attrs []slog.Attr
}

func newRingHandler(inner slog.Handler, ring *RingBuffer) *ringHandler {
	return &ringHandler{inner: inner, ring: ring}
}

func (h *ringHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= ringCaptureFloor || h.inner.Enabled(ctx, level)
}

func (h *ringHandler) Handle(ctx context.Context, r slog.Record) error {
	h.ring.append(CapturedRecord{
		Time:    r.Time,
		Level:   r.Level.String(),
		Message: r.Message,
		Attrs:   h.flattenedAttrs(r),
	})
	if h.inner.Enabled(ctx, r.Level) {
		return h.inner.Handle(ctx, r)
	}
	return nil
}

func (h *ringHandler) flattenedAttrs(r slog.Record) map[string]string {
	attrs := make(map[string]string, r.NumAttrs()+len(h.attrs))
	for _, a := range h.attrs {
		flattenAttr(attrs, "", a)
	}
	r.Attrs(func(a slog.Attr) bool {
		flattenAttr(attrs, "", a)
		return true
	})
	return attrs
}

func (h *ringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ringHandler{
		inner: h.inner.WithAttrs(attrs),
		ring:  h.ring,
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
