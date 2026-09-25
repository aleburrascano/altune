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
	available   atomic.Bool

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

func (f *Feed) Subscribe() (<-chan TapEvent, func(), error) {
	return f.broadcaster.subscribe()
}
