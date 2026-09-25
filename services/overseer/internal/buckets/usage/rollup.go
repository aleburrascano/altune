package usage

import (
	"altune/overseer/internal/core"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Rollup caps for the usage aggregator. Every rollup structure is bounded by one
// of these constants, so intake stays bounded no matter how long the stream runs
// or how many distinct queries/kinds arrive — the "bounded rollups, aggregate on
// ingest" invariant made real at the leaf.
const (
	// searchKeyCap bounds the number of distinct search queries tracked at once.
	// When full, the lowest-count query is evicted to make room, so a flood of
	// unique queries can never grow the map without limit.
	searchKeyCap = 64
	// playKindCap bounds the number of distinct play kinds tracked. The known set
	// is small; the cap defends against an unexpectedly wide kind space.
	playKindCap = 32
	// searchTopN is how many top queries Render surfaces.
	searchTopN = 10
	// timelineWindows is the number of completed activity windows the ring retains.
	timelineWindows = 60
	// timelineWindow is the width of one activity-timeline bucket.
	timelineWindow = time.Minute
)

// countMap is a bounded key→count aggregator. It increments existing keys
// forever but caps the number of distinct keys: once at capacity, adding a new
// key first evicts the current lowest-count key. Bounded by construction, so
// N×capacity distinct inputs never grow it past cap.
type countMap struct {
	counts  map[string]int
	cap     int
	evicted int
}

func newCountMap(capacity int) *countMap {
	if capacity < 1 {
		capacity = 1
	}
	return &countMap{counts: make(map[string]int), cap: capacity}
}

// add records one observation of key. An empty key is ignored so blank subjects
// never occupy a tracked slot.
func (c *countMap) add(key string) {
	if key == "" {
		return
	}
	if _, ok := c.counts[key]; ok {
		c.counts[key]++
		return
	}
	if len(c.counts) >= c.cap {
		c.evictMin()
	}
	c.counts[key] = 1
}

// evictMin removes one lowest-count entry so a new key can be admitted while the
// map stays at or below cap. Ties break on key order for determinism.
func (c *countMap) evictMin() {
	minKey := ""
	minCount := 0
	first := true
	for k, v := range c.counts {
		if first || v < minCount || (v == minCount && k < minKey) {
			minKey, minCount, first = k, v, false
		}
	}
	if !first {
		delete(c.counts, minKey)
		// Saturate so a counter that overflowed to a negative could never read as
		// "no keys dropped" and hide the cardinality truncation it exists to report.
		if c.evicted < math.MaxInt {
			c.evicted++
		}
	}
}

// evictions is how many distinct keys the map has dropped to stay under cap, so a
// render can show that a flood of one-off keys truncated the tracked set.
func (c *countMap) evictions() int { return c.evicted }

// entry is one key and its count for rendering.
type entry struct {
	Key   string
	Count int
}

// top returns the highest-count entries, at most n (n < 0 means all), sorted by
// count desc then key asc for a stable render.
func (c *countMap) top(n int) []entry {
	out := make([]entry, 0, len(c.counts))
	for k, v := range c.counts {
		out = append(out, entry{Key: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	if n >= 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

func (c *countMap) len() int { return len(c.counts) }

// timeline is a bounded activity-over-time rollup. Events fold into a current
// fixed-width window; when time crosses into a later window the completed window's
// count is flushed as one Signal into a core.RingStore, which caps the number of
// retained windows by construction. Only completed-window counts are stored — the
// raw events are never retained. The window count rides in the Signal's Text field
// (the shared Signal shape has no numeric field), parsed back on render.
type timeline struct {
	store    *core.RingStore
	window   time.Duration
	curStart time.Time
	curCount int
}

func newTimeline() *timeline {
	return &timeline{store: core.NewRingStore(timelineWindows), window: timelineWindow}
}

// record folds one event at time at into the timeline. Events at or after the
// current window's successor flush the current window and open a new one; an
// out-of-order (older) event folds into the current window rather than rewinding,
// keeping the structure monotonic and bounded.
func (t *timeline) record(at time.Time) {
	slot := at.Truncate(t.window)
	if t.curStart.IsZero() {
		t.curStart = slot
		t.curCount = 1
		return
	}
	if slot.After(t.curStart) {
		t.flush()
		t.curStart = slot
		t.curCount = 1
		return
	}
	t.curCount++
}

// flush stores the current window's count as one ring entry, count encoded in Text.
func (t *timeline) flush() {
	t.store.Add(core.Signal{At: t.curStart, Kind: "activity", Text: strconv.Itoa(t.curCount)})
}

// windows returns the retained completed windows oldest-first plus the current
// in-progress window (if any), for rendering.
func (t *timeline) windows() []entry {
	snap := t.store.Snapshot()
	out := make([]entry, 0, len(snap)+1)
	for _, s := range snap {
		n, _ := strconv.Atoi(s.Text)
		out = append(out, entry{Key: s.At.Format("15:04"), Count: n})
	}
	if !t.curStart.IsZero() {
		out = append(out, entry{Key: t.curStart.Format("15:04"), Count: t.curCount})
	}
	return out
}

// storedWindows is the number of completed windows retained (never exceeds the
// ring cap); used by tests to prove the timeline stays bounded.
func (t *timeline) storedWindows() int { return t.store.Len() }

type windowTotals struct {
	at       time.Time
	requests int
	users    int
}

type activityWindow struct {
	window   time.Duration
	curStart time.Time
	curCount int
	curUsers map[string]struct{}
}

func newActivityWindow(window time.Duration) *activityWindow {
	return &activityWindow{window: window, curUsers: make(map[string]struct{})}
}

func (w *activityWindow) add(at time.Time, user string) (windowTotals, bool) {
	slot := at.Truncate(w.window)
	var totals windowTotals
	ok := false
	if w.curStart.IsZero() {
		w.reset(slot)
	} else if slot.After(w.curStart) {
		totals = windowTotals{at: w.curStart, requests: w.curCount, users: len(w.curUsers)}
		ok = true
		w.reset(slot)
	}
	w.curCount++
	if user != "" {
		w.curUsers[user] = struct{}{}
	}
	return totals, ok
}

func (w *activityWindow) reset(start time.Time) {
	w.curStart = start
	w.curCount = 0
	w.curUsers = make(map[string]struct{})
}

// aggregator holds all bounded usage rollups behind one mutex: the tick goroutine
// ingests while HTTP handlers render, so every read and write is serialized to
// stay race-free.
type aggregator struct {
	mu       sync.Mutex
	searches *countMap
	plays    *countMap
	line     *timeline
}

func newAggregator() *aggregator {
	return &aggregator{
		searches: newCountMap(searchKeyCap),
		plays:    newCountMap(playKindCap),
		line:     newTimeline(),
	}
}

// ingest folds one collected signal into the bounded rollups. It classifies by
// kind: search_performed contributes its query to top-N searches when go-api
// carries one, playback kinds increment per-kind play counts, and every event
// advances the activity timeline. go-api's admin stream masks search text
// (#2585), so a search_performed signal ordinarily carries no query; falling
// back to its kind keeps the search count itself moving under that masking
// instead of the event vanishing into an empty, ignored key (#2594).
func (a *aggregator) ingest(s core.Signal) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch classify(s.Kind) {
	case catSearch:
		a.searches.add(searchKey(s))
	case catPlay:
		a.plays.add(s.Kind)
	case catOther:
	}
	a.line.record(s.At)
}

// searchKey is the top-N search key for one search_performed signal: its query
// when go-api carried one, otherwise the event kind, so a masked query still
// registers as one countable search rather than being dropped as an empty key.
func searchKey(s core.Signal) string {
	if s.Text != "" {
		return s.Text
	}
	return s.Kind
}

// view is an immutable snapshot of the rollups for one Render call.
type view struct {
	searches    []entry
	plays       []entry
	timeline    []entry
	droppedKeys int
}

func (a *aggregator) snapshot() view {
	a.mu.Lock()
	defer a.mu.Unlock()
	return view{
		searches:    a.searches.top(searchTopN),
		plays:       a.plays.top(-1),
		timeline:    a.line.windows(),
		droppedKeys: a.searches.evictions() + a.plays.evictions(),
	}
}

type category int

const (
	catOther category = iota
	catSearch
	catPlay
)

// classify maps a go-api event kind to its usage category. Search events carry
// the query as their subject; playback events are counted per kind.
func classify(kind string) category {
	switch kind {
	case "search_performed":
		return catSearch
	case "play", "skip", "completed", "library_add", "wrong_album", "playback_health":
		return catPlay
	default:
		return catOther
	}
}
