package usage

import (
	"altune/overseer/internal/goapi"
	"context"
	"fmt"
	"testing"
	"time"
)

// TestSearchRollupStaysBounded is the bounded-rollup proof for top-N searches:
// feeding many times the key capacity of distinct queries never grows the tracked
// map past its cap, and the surfaced top list is bounded by searchTopN.
func TestSearchRollupStaysBounded(t *testing.T) {
	src := newFakeSource(8 * searchKeyCap)
	b := newBucket(src)
	for i := 0; i < 8*searchKeyCap; i++ {
		src.push(goapi.Event{Type: "search_performed", Subject: fmt.Sprintf("q-%d", i)})
	}
	drainAll(t, b)

	if got := b.roll.searches.len(); got > searchKeyCap {
		t.Errorf("search map holds %d distinct keys, want capped at %d", got, searchKeyCap)
	}
	if got := len(b.roll.snapshot().searches); got > searchTopN {
		t.Errorf("render surfaced %d searches, want capped at %d", got, searchTopN)
	}
}

// TestPlayRollupStaysBounded proves per-kind play counts stay capped even under a
// flood of unexpected distinct kinds.
func TestPlayRollupStaysBounded(t *testing.T) {
	src := newFakeSource(8 * playKindCap)
	b := newBucket(src)
	for i := 0; i < 8*playKindCap; i++ {
		// Distinct fabricated play-family kinds classify as catPlay only for the
		// known set; unknown kinds fall to catOther. Use the known "play" kind plus
		// a wide fabricated space via subject to exercise the countMap cap directly.
		b.roll.plays.add(fmt.Sprintf("kind-%d", i))
	}
	if got := b.roll.plays.len(); got > playKindCap {
		t.Errorf("play map holds %d distinct kinds, want capped at %d", got, playKindCap)
	}
}

// TestTimelineStaysBounded is the bounded-rollup proof for the activity timeline:
// events spread across far more windows than the ring holds never grow retained
// storage past timelineWindows.
func TestTimelineStaysBounded(t *testing.T) {
	src := newFakeSource(8 * timelineWindows)
	b := newBucket(src)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 8*timelineWindows; i++ {
		// One event per distinct window pushes a new completed window each step.
		src.push(goapi.Event{Type: "play", Timestamp: base.Add(time.Duration(i) * timelineWindow)})
	}
	drainAll(t, b)

	if got := b.roll.line.storedWindows(); got > timelineWindows {
		t.Errorf("timeline retained %d windows, want capped at %d", got, timelineWindows)
	}
	if got := len(b.roll.snapshot().timeline); got > timelineWindows+1 {
		t.Errorf("timeline rendered %d windows, want <= %d", got, timelineWindows+1)
	}
}

// TestTopNOrders proves top() ranks by count desc then key asc so the busiest
// queries surface first, deterministically.
func TestTopNOrders(t *testing.T) {
	c := newCountMap(16)
	for i := 0; i < 5; i++ {
		c.add("hot")
	}
	for i := 0; i < 3; i++ {
		c.add("warm")
	}
	c.add("cold")
	got := c.top(2)
	if len(got) != 2 || got[0].Key != "hot" || got[1].Key != "warm" {
		t.Fatalf("top(2) = %+v, want [hot warm]", got)
	}
	if got[0].Count != 5 || got[1].Count != 3 {
		t.Errorf("top counts = %d,%d want 5,3", got[0].Count, got[1].Count)
	}
}

// TestEvictKeepsBoundAndPrefersFrequent proves eviction drops the lowest-count
// key, so a one-off query cannot push out a repeatedly-searched one under load.
func TestEvictKeepsBoundAndPrefersFrequent(t *testing.T) {
	c := newCountMap(2)
	for i := 0; i < 10; i++ {
		c.add("frequent")
	}
	c.add("rare-a") // fills the second slot
	c.add("rare-b") // at cap → evicts a rare one, never "frequent"
	if c.len() > 2 {
		t.Fatalf("map len = %d, want capped at 2", c.len())
	}
	if _, ok := c.counts["frequent"]; !ok {
		t.Errorf("eviction dropped the frequent key: %+v", c.counts)
	}
}

// drainAll runs collect/store cycles until every queued event is folded into the
// rollups, the way the tick loop drains the source over successive ticks.
func drainAll(t *testing.T, b *Bucket) {
	t.Helper()
	for i := 0; i < 100; i++ {
		signals, err := b.Collect(context.Background())
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		b.Store(signals)
		if len(signals) == 0 {
			return
		}
	}
}
