package logging

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

type correlationIDKey struct{}

func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey{}, id)
}

func CorrelationIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(correlationIDKey{}).(string)
	return id
}

func CorrelationAttr(ctx context.Context) slog.Attr {
	return slog.String(KeyCorrelationID, CorrelationIDFromContext(ctx))
}

type correlationHandler struct {
	inner slog.Handler
}

func newCorrelationHandler(inner slog.Handler) *correlationHandler {
	return &correlationHandler{inner: inner}
}

func (h *correlationHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *correlationHandler) Handle(ctx context.Context, r slog.Record) error {
	var stamps []slog.Attr
	if id := CorrelationIDFromContext(ctx); id != "" && !recordHasKey(r, KeyCorrelationID) {
		stamps = append(stamps, slog.String(KeyCorrelationID, id))
	}
	if span := trace.SpanContextFromContext(ctx); span.HasTraceID() && !recordHasKey(r, KeyTraceID) {
		stamps = append(stamps, slog.String(KeyTraceID, span.TraceID().String()))
	}
	r = r.Clone()
	r.AddAttrs(stamps...)
	return h.inner.Handle(ctx, r)
}

func (h *correlationHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &correlationHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *correlationHandler) WithGroup(name string) slog.Handler {
	return &correlationHandler{inner: h.inner.WithGroup(name)}
}

func recordHasKey(r slog.Record, key string) bool {
	has := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			has = true
			return false
		}
		return true
	})
	return has
}
