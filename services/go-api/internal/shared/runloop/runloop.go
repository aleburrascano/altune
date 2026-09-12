package runloop

import (
	"context"
	"sync/atomic"
)

type Background struct {
	cancel context.CancelFunc
	done   chan struct{}
	paused atomic.Bool
}

// Pause engages a runtime kill switch: loops that honor Paused stop doing work
// on each tick without tearing down the goroutine, so they can be disabled
// without a redeploy.
func (b *Background) Pause() { b.paused.Store(true) }

// Resume clears the kill switch so subsequent ticks do their work again.
func (b *Background) Resume() { b.paused.Store(false) }

// Paused reports whether the loop is currently gated off.
func (b *Background) Paused() bool { return b.paused.Load() }

func (b *Background) Spawn(ctx context.Context, loop func(context.Context)) {
	loopCtx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	b.done = make(chan struct{})
	go func() {
		defer close(b.done)
		loop(loopCtx)
	}()
}

func (b *Background) Shutdown(ctx context.Context) {
	if b.cancel == nil {
		return
	}
	b.cancel()
	select {
	case <-b.done:
	case <-ctx.Done():
	}
}
