package logging

import (
	"context"
	"fmt"
	"log/slog"
	"unicode/utf8"
)

const ringCaptureFloor = slog.LevelDebug

type ringHandler struct {
	inner  slog.Handler
	ring   *RingBuffer
	attrs  []slog.Attr
	prefix string
}

func newRingHandler(inner slog.Handler, ring *RingBuffer) *ringHandler {
	return &ringHandler{inner: inner, ring: ring}
}

func (h *ringHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= ringCaptureFloor || h.inner.Enabled(ctx, level)
}

func (h *ringHandler) Handle(ctx context.Context, r slog.Record) error {
	r = scrubbedRecord(r)
	h.ring.append(CapturedRecord{
		Time:    r.Time,
		Level:   r.Level.String(),
		Message: capRingText(r.Message),
		Attrs:   h.flattenedAttrs(r),
	})
	if h.inner.Enabled(ctx, r.Level) {
		return h.inner.Handle(ctx, r)
	}
	return nil
}

func (h *ringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	safe := withoutSensitiveLeaves("", attrs)
	if len(safe) == 0 {
		return h
	}
	return &ringHandler{
		inner:  h.inner.WithAttrs(safe),
		ring:   h.ring,
		attrs:  append(append([]slog.Attr{}, h.attrs...), h.prefixed(safe)...),
		prefix: h.prefix,
	}
}

func (h *ringHandler) WithGroup(name string) slog.Handler {
	return &ringHandler{
		inner:  h.inner.WithGroup(name),
		ring:   h.ring,
		attrs:  h.attrs,
		prefix: dottedKey(h.prefix, name),
	}
}

func (h *ringHandler) prefixed(attrs []slog.Attr) []slog.Attr {
	if h.prefix == "" {
		return attrs
	}
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = slog.Attr{Key: dottedKey(h.prefix, a.Key), Value: a.Value}
	}
	return out
}

const ringTextCapBytes = 2048

func capRingText(s string) string {
	if len(s) <= ringTextCapBytes {
		return s
	}
	cut := ringTextCapBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return fmt.Sprintf("%s…[truncated %d]", s[:cut], len(s)-cut)
}

func (h *ringHandler) flattenedAttrs(r slog.Record) map[string]string {
	attrs := make(map[string]string, r.NumAttrs()+len(h.attrs))
	for _, a := range h.attrs {
		flattenAttr(attrs, "", a)
	}
	r.Attrs(func(a slog.Attr) bool {
		flattenAttr(attrs, h.prefix, a)
		return true
	})
	return attrs
}

func dottedKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func flattenAttr(dst map[string]string, prefix string, a slog.Attr) {
	resolved := a.Value.Resolve()
	key := dottedKey(prefix, a.Key)
	if resolved.Kind() == slog.KindGroup {
		for _, ga := range resolved.Group() {
			flattenAttr(dst, key, ga)
		}
		return
	}
	if isSensitiveLeaf(key, resolved) {
		return
	}
	dst[key] = capRingText(scrubSecrets(resolved.String()))
}
