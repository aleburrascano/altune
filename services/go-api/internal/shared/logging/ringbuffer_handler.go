package logging

import (
	"context"
	"log/slog"
)

const ringCaptureFloor = slog.LevelDebug

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
	r = withoutSensitiveAttrs(r)
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
