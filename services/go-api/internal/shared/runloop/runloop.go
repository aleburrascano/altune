package runloop

import (
	"context"
	"sync"
	"sync/atomic"
)

// Background owns at most one loop goroutine for its whole lifetime. Spawn runs
// on the leader-election goroutine while Shutdown runs on the app shutdown
// path, so the two may be called concurrently and in either order.
type Background struct {
	mu      sync.Mutex
	current *run
	stopped bool

	paused atomic.Bool
}

// run is a live loop goroutine's handle: the cancel that stops it and the
// channel it closes on return. Both exist or neither does.
type run struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// Pause engages a runtime kill switch: loops that honor Paused stop doing work
// on each tick without tearing down the goroutine, so they can be disabled
// without a redeploy.
func (b *Background) Pause() { b.paused.Store(true) }

// Resume clears the kill switch so subsequent ticks do their work again.
func (b *Background) Resume() { b.paused.Store(false) }

// Paused reports whether the loop is currently gated off.
func (b *Background) Paused() bool { return b.paused.Load() }

// Spawn starts loop, unless one is already running or Shutdown has already been
// called. In those two cases loop never runs at all, so a caller that acquired
// something for it (a subscription, a connection) still owns the release.
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

// claim takes the single loop slot, returning nil when the slot is already
// taken or this Background has been shut down.
func (b *Background) claim(cancel context.CancelFunc) *run {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped || b.current != nil {
		return nil
	}
	b.current = &run{cancel: cancel, done: make(chan struct{})}
	return b.current
}

// Shutdown cancels the loop and waits for it to return, for as long as ctx
// allows. A Shutdown that arrives before Spawn still takes effect: it latches
// the slot shut, so a startup racing shutdown leaves no goroutine behind.
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

// stop latches the loop slot shut and hands back the live loop, nil when none
// was ever spawned. The handle outlives the call so a second Shutdown waits on
// the same loop rather than returning while it is still unwinding.
func (b *Background) stop() *run {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopped = true
	return b.current
}
