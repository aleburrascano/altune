package runloop

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
)

type PanicError struct {
	Event string
	Value any
	Stack []byte
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("%s: panic: %v", e.Event, e.Value)
}

func Try(ctx context.Context, event string, fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			panicked := newPanicError(event, r)
			logPanic(ctx, panicked, nil)
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
		append(attrs, "panic", panicked.Value, "stack", string(panicked.Stack))...)
}
