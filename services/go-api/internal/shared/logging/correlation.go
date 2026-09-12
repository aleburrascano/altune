package logging

import (
	"context"
	"log/slog"
)

// correlationAttrKey is the log attribute under which the request's
// correlation ID is recorded. Kept stable so operators can grep for it.
const correlationAttrKey = "corr_id"

type correlationIDKey struct{}

// WithCorrelationID returns a context carrying the given correlation ID.
// It is the single source of truth for the key the context-aware handler
// reads, so any *Context log call made under this context is tagged.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey{}, id)
}

// CorrelationIDFromContext reports the correlation ID carried by ctx, if any.
func CorrelationIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(correlationIDKey{}).(string)
	return id
}

// correlationHandler is the outermost handler in the chain. It reads the
// correlation ID from the context and stamps it onto every record, so any
// InfoContext/WarnContext/ErrorContext call deep in the stack carries it
// without each call site having to add it.
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
	if id := CorrelationIDFromContext(ctx); id != "" && !recordHasKey(r, correlationAttrKey) {
		r = r.Clone()
		r.AddAttrs(slog.String(correlationAttrKey, id))
	}
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
