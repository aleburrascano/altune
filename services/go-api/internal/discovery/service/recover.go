package service

import (
	"context"
	"log/slog"
	"runtime/debug"
)

func RecoverGoroutine(ctx context.Context, event string, attrs ...any) {
	if r := recover(); r != nil {
		slog.ErrorContext(ctx, event,
			append(attrs, "panic", r, "stack", string(debug.Stack()))...)
	}
}
