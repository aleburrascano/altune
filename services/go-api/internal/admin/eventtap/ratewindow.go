package eventtap

import (
	"sync"
	"time"
)

// rateBucket totals the events of one type received within rateBucketSpan of
// at. Counting per bucket rather than keeping a timestamp each is what lets a
// type report its true rate: the window holds at most one bucket per span, so
// an arbitrarily hot type costs the same memory as a quiet one.
type rateBucket struct {
	at time.Time
	n  int
}

type rateWindow struct {
	mu     sync.Mutex
	recent map[string][]rateBucket
	// now stamps each bucket with a monotonic-bearing receive instant; since
	// measures elapsed from it. Both are monotonic-safe in production and
	// injectable so tests can simulate wall-clock jumps.
	now   func() time.Time
	since func(time.Time) time.Duration
}

func newRateWindow(now func() time.Time, since func(time.Time) time.Duration) *rateWindow {
	return &rateWindow{recent: make(map[string][]rateBucket), now: now, since: since}
}

func (w *rateWindow) append(typ string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.recent[typ] = w.count(w.within(w.recent[typ]))
}

func (w *rateWindow) counts() map[string]int {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[string]int, len(w.recent))
	for typ, buckets := range w.recent {
		kept := w.within(buckets)
		if len(kept) == 0 {
			delete(w.recent, typ)
			continue
		}
		w.recent[typ] = kept
		out[typ] = total(kept)
	}
	return out
}

// count adds one event to the newest bucket, opening a bucket first when that
// one has aged out of its span.
func (w *rateWindow) count(buckets []rateBucket) []rateBucket {
	if last := len(buckets) - 1; last >= 0 && w.since(buckets[last].at) < rateBucketSpan {
		buckets[last].n++
		return buckets
	}
	return append(buckets, rateBucket{at: w.now(), n: 1})
}

// within drops the buckets that have left the rate window. It compacts in
// place, so the caller must drop the slice it passed in.
func (w *rateWindow) within(buckets []rateBucket) []rateBucket {
	kept := buckets[:0]
	for _, b := range buckets {
		if w.since(b.at) < feedRateWindow {
			kept = append(kept, b)
		}
	}
	return kept
}

func total(buckets []rateBucket) int {
	n := 0
	for _, b := range buckets {
		n += b.n
	}
	return n
}
