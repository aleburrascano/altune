package usage

import (
	"altune/overseer/internal/core"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	searchKeyCap    = 64
	playKindCap     = 32
	searchTopN      = 10
	timelineWindows = 60
	timelineWindow  = time.Minute
)

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
		if c.evicted < math.MaxInt {
			c.evicted++
		}
	}
}

func (c *countMap) evictions() int { return c.evicted }

type entry struct {
	Key   string
	Count int
}

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

type timeline struct {
	store    *core.RingStore
	window   time.Duration
	curStart time.Time
	curCount int
}

func newTimeline() *timeline {
	return &timeline{store: core.NewRingStore(timelineWindows), window: timelineWindow}
}

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

func (t *timeline) flush() {
	t.store.Add(core.Signal{At: t.curStart, Kind: "activity", Text: strconv.Itoa(t.curCount)})
}

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

func searchKey(s core.Signal) string {
	if s.Text != "" {
		return s.Text
	}
	return s.Kind
}

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
