package background

import (
	"altune/go-api/internal/shared/redact"
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
)

type PanicError struct {
	Event string
	Value any
	Stack []byte
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("%s: panic: %s", e.Event, e.loggable())
}

func (e *PanicError) loggable() string {
	return redact.LogText(fmt.Sprint(e.Value))
}

func SafeRun(ctx context.Context, event string, fn func(), attrs ...any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			panicked := newPanicError(event, r)
			logPanic(ctx, panicked, attrs)
			err = panicked
		}
	}()
	fn()
	return nil
}

func RecoverContext(ctx context.Context, event string, attrs ...any) {
	if r := recover(); r != nil {
		logPanic(ctx, newPanicError(event, r), attrs)
	}
}

func newPanicError(event string, value any) *PanicError {
	return &PanicError{Event: event, Value: value, Stack: debug.Stack()}
}

func logPanic(ctx context.Context, panicked *PanicError, attrs []any) {
	slog.ErrorContext(ctx, panicked.Event,
		append(slices.Clip(attrs), "panic", panicked.loggable(), "stack", string(panicked.Stack))...)
}
