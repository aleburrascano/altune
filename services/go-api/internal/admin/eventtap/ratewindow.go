package eventtap

import (
	"sync"
	"time"
)

type rateWindow struct {
	mu     sync.Mutex
	recent map[string][]time.Time
	// now stamps each event with a monotonic-bearing receive instant; since
	// measures elapsed from it. Both are monotonic-safe in production and
	// injectable so tests can simulate wall-clock jumps.
	now   func() time.Time
	since func(time.Time) time.Duration
}

func newRateWindow(now func() time.Time, since func(time.Time) time.Duration) *rateWindow {
	return &rateWindow{recent: make(map[string][]time.Time), now: now, since: since}
}

func (w *rateWindow) append(typ string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	kept := w.prune(w.recent[typ])
	kept = append(kept, w.now())
	if len(kept) > perTypeCap {
		kept = kept[len(kept)-perTypeCap:]
	}
	w.recent[typ] = kept
}

func (w *rateWindow) counts() map[string]int {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[string]int, len(w.recent))
	for typ, times := range w.recent {
		if n := w.countWithin(times); n > 0 {
			out[typ] = n
		}
	}
	return out
}

func (w *rateWindow) prune(times []time.Time) []time.Time {
	kept := times[:0]
	for _, t := range times {
		if w.since(t) < feedRateWindow {
			kept = append(kept, t)
		}
	}
	return kept
}

func (w *rateWindow) countWithin(times []time.Time) int {
	n := 0
	for _, t := range times {
		if w.since(t) < feedRateWindow {
			n++
		}
	}
	return n
}
