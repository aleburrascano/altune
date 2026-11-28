package service

import (
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"sync/atomic"
)

const feedSubSize = 64

type EventFeed struct {
	broadcaster *broadcaster
	available   atomic.Bool

	background.Background
}

func NewEventFeed() *EventFeed {
	return &EventFeed{broadcaster: newBroadcaster(MaxSubscribers)}
}

func (f *EventFeed) Start(ctx context.Context, tap *EventTap) {
	ch, cancelTap, err := tap.SubscribeAll()
	if err != nil {
		slog.ErrorContext(ctx, "admin.event_feed_unavailable", logging.KeyError, err)
		return
	}
	f.available.Store(true)
	f.Spawn(ctx, func(loopCtx context.Context) {
		defer f.releaseTap(cancelTap)
		defer f.broadcaster.closeAll()
		f.loop(loopCtx, ch)
	})
}

func (f *EventFeed) releaseTap(cancelTap func()) {
	f.available.Store(false)
	cancelTap()
}

func (f *EventFeed) Available() bool {
	return f.available.Load()
}

func (f *EventFeed) loop(ctx context.Context, ch <-chan TapEvent) {
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

func (f *EventFeed) record(evt TapEvent) {
	f.broadcaster.broadcast(evt)
}

func (f *EventFeed) Subscribe() (<-chan TapEvent, func(), error) {
	return f.broadcaster.subscribe()
}

func (f *EventFeed) Dropped() uint64 {
	return f.broadcaster.droppedCount()
}
