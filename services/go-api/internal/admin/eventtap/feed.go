package eventtap

import (
	"altune/go-api/internal/shared/runloop"
	"context"
	"log/slog"
	"sync/atomic"
)

const feedSubSize = 64

type Feed struct {
	broadcaster *broadcaster
	// available is true only between a successful subscribe and the loop's
	// return. Outside that window the feed records nothing, which callers must
	// be able to tell apart from a system with nothing to report.
	available atomic.Bool

	runloop.Background
}

func NewFeed() *Feed {
	return &Feed{broadcaster: newBroadcaster(MaxSubscribers)}
}

func (f *Feed) Start(ctx context.Context, tap *Tap) {
	ch, cancelTap, err := tap.SubscribeAll()
	if err != nil {
		slog.Error("admin.event_feed_unavailable", "error", err)
		return
	}
	f.available.Store(true)
	f.Spawn(ctx, func(loopCtx context.Context) {
		defer f.releaseTap(cancelTap)
		f.loop(loopCtx, ch)
	})
}

func (f *Feed) releaseTap(cancelTap func()) {
	f.available.Store(false)
	cancelTap()
}

// Available reports whether this feed is draining a tap. It is false when Start
// could not subscribe and after the loop returns: the feed then has no events
// to serve, and a caller must surface that rather than serve emptiness.
func (f *Feed) Available() bool {
	return f.available.Load()
}

func (f *Feed) loop(ctx context.Context, ch <-chan TapEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			f.record(evt)
		}
	}
}

func (f *Feed) record(evt TapEvent) {
	f.broadcaster.broadcast(evt)
}

// Subscribe opens a live feed subscription. It returns ErrTooManySubscribers,
// and no channel, once MaxSubscribers subscriptions are open; the returned
// cancel func must be called to release the slot.
func (f *Feed) Subscribe() (<-chan TapEvent, func(), error) {
	return f.broadcaster.subscribe()
}
