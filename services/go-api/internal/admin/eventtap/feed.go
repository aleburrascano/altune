package eventtap

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	feedRateWindow = 60 * time.Second
	feedSubSize    = 64
	// perTypeCap bounds memory between Rates() prunes if one type fires heavily.
	perTypeCap = 1024
)

// Feed is the single consumer of the bus system-wide tap. It keeps per-type
// rolling rates and fans the redacted events out to connected SSE clients. All
// state is guarded by one mutex; only the loop goroutine and handler calls
// touch it.
type Feed struct {
	mu      sync.Mutex
	recent  map[string][]time.Time
	subs    map[int]chan TapEvent
	nextSub int

	cancel context.CancelFunc
	done   chan struct{}
}

func NewFeed() *Feed {
	return &Feed{
		recent: make(map[string][]time.Time),
		subs:   make(map[int]chan TapEvent),
	}
}

// Start subscribes to the tap and begins consuming. If the tap already has a
// consumer the feed degrades to empty (logs, does not crash).
func (f *Feed) Start(ctx context.Context, tap *Tap) {
	ch, cancelTap, err := tap.SubscribeAll()
	if err != nil {
		slog.Error("admin.event_feed_unavailable", "error", err)
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	f.cancel = func() {
		cancel()
		cancelTap()
	}
	f.done = make(chan struct{})
	go f.loop(loopCtx, ch)
}

func (f *Feed) loop(ctx context.Context, ch <-chan TapEvent) {
	defer close(f.done)
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
	f.mu.Lock()
	times := append(f.recent[evt.Type], evt.Timestamp)
	if len(times) > perTypeCap {
		times = times[len(times)-perTypeCap:]
	}
	f.recent[evt.Type] = times
	for _, ch := range f.subs {
		select {
		case ch <- evt:
		default:
		}
	}
	f.mu.Unlock()
}

// Rates returns per-type event counts within the rolling window, pruning stale
// timestamps as it goes.
func (f *Feed) Rates() map[string]int {
	cutoff := time.Now().UTC().Add(-feedRateWindow)
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]int, len(f.recent))
	for typ, times := range f.recent {
		kept := times[:0]
		for _, t := range times {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		f.recent[typ] = kept
		if len(kept) > 0 {
			out[typ] = len(kept)
		}
	}
	return out
}

// Subscribe registers one SSE client on the fan-out and returns its channel
// plus a cancel that unregisters and closes it.
func (f *Feed) Subscribe() (<-chan TapEvent, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.nextSub
	f.nextSub++
	ch := make(chan TapEvent, feedSubSize)
	f.subs[id] = ch
	return ch, func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if c, ok := f.subs[id]; ok {
			delete(f.subs, id)
			close(c)
		}
	}
}

// Shutdown stops the feed loop, waiting up to the context deadline.
func (f *Feed) Shutdown(ctx context.Context) {
	if f.cancel == nil {
		return
	}
	f.cancel()
	select {
	case <-f.done:
	case <-ctx.Done():
	}
}
