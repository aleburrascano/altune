package runloop

import (
	"context"
	"time"
)

func Detach(ctx context.Context, budget time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), budget)
}

func WaitWithin(ctx context.Context, wait func()) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		wait()
	}()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}
