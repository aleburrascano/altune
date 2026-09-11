package runloop

import "context"

type Background struct {
	cancel context.CancelFunc
	done   chan struct{}
}

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
