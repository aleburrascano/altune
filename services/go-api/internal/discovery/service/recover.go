package service

import (
	"context"
	"log/slog"
	"runtime/debug"
)

// RecoverGoroutine contains a panic in a goroutine spawned around a provider or
// port call, logging it as a failure of that one operation instead of letting it
// terminate the process. Go only recovers panics in the goroutine that defers
// the recover, and net/http's per-request recovery does not reach child
// goroutines, so every such spawn must defer this directly:
//
//	go func() {
//		defer wg.Done()
//		defer RecoverGoroutine(ctx, "event.name", "provider", name)
//		...
//	}()
//
// The recovered panic is only logged; callers that must react to it (for
// example to surface an error) recover inline instead.
func RecoverGoroutine(ctx context.Context, event string, attrs ...any) {
	if r := recover(); r != nil {
		slog.ErrorContext(ctx, event,
			append(attrs, "panic", r, "stack", string(debug.Stack()))...)
	}
}
