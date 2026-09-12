package eventtap

import (
	"context"
	"log/slog"
	"time"

	"altune/go-api/internal/shared/runloop"
)

const (
	feedRateWindow = 60 * time.Second
	feedSubSize    = 64
	perTypeCap     = 1024
)

type Feed struct {
	rates       *rateWindow
	broadcaster *broadcaster

	runloop.Background
}

func NewFeed() *Feed {
	return &Feed{
		rates:       newRateWindow(),
		broadcaster: newBroadcaster(),
	}
}

func (f *Feed) Start(ctx context.Context, tap *Tap) {
	ch, cancelTap, err := tap.SubscribeAll()
	if err != nil {
		slog.Error("admin.event_feed_unavailable", "error", err)
		return
	}
	f.Spawn(ctx, func(loopCtx context.Context) {
		defer cancelTap()
		f.loop(loopCtx, ch)
	})
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
	f.rates.append(evt.Type, evt.Timestamp)
	f.broadcaster.broadcast(evt)
}

func (f *Feed) Rates() map[string]int {
	return f.rates.countsSince(time.Now().UTC().Add(-feedRateWindow))
}

func (f *Feed) Subscribe() (<-chan TapEvent, func()) {
	return f.broadcaster.subscribe()
}
