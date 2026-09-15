package eventtap

import (
	"context"
	"log/slog"
	"sync/atomic"
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
	// tap is the Tap this feed drains, set once Start subscribes, so Dropped
	// can report the tap's overflow count.
	tap atomic.Pointer[Tap]

	runloop.Background
}

func NewFeed() *Feed {
	return newFeedWithClock(time.Now, time.Since)
}

func newFeedWithClock(now func() time.Time, since func(time.Time) time.Duration) *Feed {
	return &Feed{
		rates:       newRateWindow(now, since),
		broadcaster: newBroadcaster(MaxSubscribers),
	}
}

func (f *Feed) Start(ctx context.Context, tap *Tap) {
	ch, cancelTap, err := tap.SubscribeAll()
	if err != nil {
		slog.Error("admin.event_feed_unavailable", "error", err)
		return
	}
	f.tap.Store(tap)
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
	f.rates.append(evt.Type)
	f.broadcaster.broadcast(evt)
}

func (f *Feed) Rates() map[string]int {
	return f.rates.counts()
}

// Dropped reports how many events the subscribed tap discarded because this
// feed's channel was full, cumulative since process start. It is zero before
// Start subscribes (or when Start could not subscribe).
func (f *Feed) Dropped() uint64 {
	tap := f.tap.Load()
	if tap == nil {
		return 0
	}
	return tap.Dropped()
}

// Subscribe opens a live feed subscription. It returns ErrTooManySubscribers,
// and no channel, once MaxSubscribers subscriptions are open; the returned
// cancel func must be called to release the slot.
func (f *Feed) Subscribe() (<-chan TapEvent, func(), error) {
	return f.broadcaster.subscribe()
}
