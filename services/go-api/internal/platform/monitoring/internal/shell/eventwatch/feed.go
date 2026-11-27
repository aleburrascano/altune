package eventwatch

import (
	"altune/go-api/internal/shared/background"
	"context"
	"log/slog"
	"sync/atomic"
)

const feedSubSize = 64

type WatchedEvents struct {
	broadcaster *broadcaster
	available   atomic.Bool

	background.Background
}

func NewFeed() *WatchedEvents {
	return &WatchedEvents{broadcaster: newBroadcaster(MaxSubscribers)}
}

func (f *WatchedEvents) Start(ctx context.Context, tap *EventWatcher) {
	ch, cancelTap, err := tap.SubscribeAll()
	if err != nil {
		slog.ErrorContext(ctx, "admin.event_feed_unavailable", "error", err)
		return
	}
	f.available.Store(true)
	f.Spawn(ctx, func(loopCtx context.Context) {
		defer f.releaseTap(cancelTap)
		defer f.broadcaster.closeAll()
		f.loop(loopCtx, ch)
	})
}

func (f *WatchedEvents) releaseTap(cancelTap func()) {
	f.available.Store(false)
	cancelTap()
}

func (f *WatchedEvents) Available() bool {
	return f.available.Load()
}

func (f *WatchedEvents) loop(ctx context.Context, ch <-chan TapEvent) {
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

func (f *WatchedEvents) record(evt TapEvent) {
	f.broadcaster.broadcast(evt)
}

func (f *WatchedEvents) Subscribe() (<-chan TapEvent, func(), error) {
	return f.broadcaster.subscribe()
}

func (f *WatchedEvents) Dropped() uint64 {
	return f.broadcaster.droppedCount()
}
