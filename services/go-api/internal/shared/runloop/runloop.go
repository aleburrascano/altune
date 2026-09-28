package runloop

import (
	"context"
	"sync"
	"sync/atomic"
)

type Background struct {
	mu      sync.Mutex
	current *run
	stopped bool

	paused atomic.Bool
}

type run struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (b *Background) Pause() { b.paused.Store(true) }

func (b *Background) Resume() { b.paused.Store(false) }

func (b *Background) Paused() bool { return b.paused.Load() }

func (b *Background) Spawn(ctx context.Context, loop func(context.Context)) {
	loopCtx, cancel := context.WithCancel(ctx)
	claimed := b.claim(cancel)
	if claimed == nil {
		cancel()
		return
	}
	go func() {
		defer close(claimed.done)
		loop(loopCtx)
	}()
}

func (b *Background) claim(cancel context.CancelFunc) *run {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped || b.current != nil {
		return nil
	}
	b.current = &run{cancel: cancel, done: make(chan struct{})}
	return b.current
}

func (b *Background) Shutdown(ctx context.Context) {
	current := b.stop()
	if current == nil {
		return
	}
	current.cancel()
	select {
	case <-current.done:
	case <-ctx.Done():
	}
}

func (b *Background) stop() *run {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopped = true
	return b.current
}
