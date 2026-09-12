package eventtap

import (
	"sync"
	"time"
)

type rateWindow struct {
	mu     sync.Mutex
	recent map[string][]time.Time
}

func newRateWindow() *rateWindow {
	return &rateWindow{recent: make(map[string][]time.Time)}
}

func (w *rateWindow) append(typ string, at time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	kept := prune(w.recent[typ], at.Add(-feedRateWindow))
	kept = append(kept, at)
	if len(kept) > perTypeCap {
		kept = kept[len(kept)-perTypeCap:]
	}
	w.recent[typ] = kept
}

func (w *rateWindow) countsSince(cutoff time.Time) map[string]int {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[string]int, len(w.recent))
	for typ, times := range w.recent {
		n := countAfter(times, cutoff)
		if n > 0 {
			out[typ] = n
		}
	}
	return out
}

func prune(times []time.Time, cutoff time.Time) []time.Time {
	kept := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	return kept
}

func countAfter(times []time.Time, cutoff time.Time) int {
	n := 0
	for _, t := range times {
		if t.After(cutoff) {
			n++
		}
	}
	return n
}
