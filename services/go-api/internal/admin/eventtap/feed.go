package eventtap

import (
	"altune/go-api/internal/shared/runloop"
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

const (
	feedRateWindow = 60 * time.Second
	feedSubSize    = 64
	rateBucketSpan = time.Second
)

// Feed drains one Tap into the counts and live subscriptions the admin event
// routes serve. It is safe for concurrent use: its rateWindow and broadcaster
// each hold their own lock, and the rest of its state is atomic.
type Feed struct {
	rates       *rateWindow
	broadcaster *broadcaster
	// tap is the Tap this feed drains, set once Start subscribes, so Dropped
	// can report the tap's overflow count.
	tap atomic.Pointer[Tap]
	// available is true only between a successful subscribe and the loop's
	// return. Outside that window the feed records nothing, which callers must
	// be able to tell apart from a system with nothing to report.
	available atomic.Bool

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

// Start subscribes to tap and drains it until ctx ends. It returns silently
// when the tap already has a subscriber, and the feed then stays unavailable
// for good: Rates keeps reporting nothing and a channel handed out by Subscribe
// never delivers an event. Available, not that emptiness, is what tells a
// caller apart from an idle system.
func (f *Feed) Start(ctx context.Context, tap *Tap) {
	ch, cancelTap, err := tap.SubscribeAll()
	if err != nil {
		slog.Error("admin.event_feed_unavailable", "error", err)
		return
	}
	f.tap.Store(tap)
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
