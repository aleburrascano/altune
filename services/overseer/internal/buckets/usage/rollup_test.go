package usage_test

import (
	"altune/overseer/internal/buckets/usage"
	"altune/overseer/internal/goapi"
	"context"
	"fmt"
	"testing"
	"time"
)

const (
	searchKeyCap    = 64
	searchTopN      = 10
	timelineWindows = 60
)

func search(src *fakeSource, query string, times int) {
	for range times {
		src.push(goapi.Event{Type: "search_performed", Subject: query})
	}
}

func TestSearchRollupStaysBounded(t *testing.T) {
	const pushed = 8 * searchKeyCap
	src := newFakeSource(pushed)
	b := newBucket(src)
	for i := range pushed {
		search(src, fmt.Sprintf("q-%d", i), 1)
	}
	drainAll(t, b)

	d := snapData(t, b.Snapshot())
	if got := d.DroppedKeys; got != pushed-searchKeyCap {
		t.Errorf("dropped keys = %d, want %d: the search map keeps at most %d distinct queries", got, pushed-searchKeyCap, searchKeyCap)
	}
	if got := len(d.Searches); got != searchTopN {
		t.Errorf("render surfaced %d searches, want the top %d", got, searchTopN)
	}
}

func TestTimelineStaysBounded(t *testing.T) {
	src := newFakeSource(8 * timelineWindows)
	b := newBucket(src)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 8 * timelineWindows {
		src.push(goapi.Event{Type: "play", Timestamp: base.Add(time.Duration(i) * time.Minute)})
	}
	drainAll(t, b)

	if got := len(snapData(t, b.Snapshot()).Timeline); got != timelineWindows+1 {
		t.Errorf("timeline rendered %d windows, want the %d kept plus the open one", got, timelineWindows)
	}
}

func TestSearchesRankByCountThenByQuery(t *testing.T) {
	src := newFakeSource(16)
	b := newBucket(src)
	search(src, "cold-b", 1)
	search(src, "warm", 3)
	search(src, "cold-a", 1)
	search(src, "hot", 5)
	drainAll(t, b)

	got := snapData(t, b.Snapshot()).Searches
	want := []usage.Count{{Label: "hot", Count: 5}, {Label: "warm", Count: 3}, {Label: "cold-a", Count: 1}, {Label: "cold-b", Count: 1}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("searches = %+v, want %+v", got, want)
	}
}

func TestEvictionKeepsTheFrequentQuery(t *testing.T) {
	src := newFakeSource(16 + searchKeyCap)
	b := newBucket(src)
	search(src, "frequent", 10)
	for i := range searchKeyCap {
		search(src, fmt.Sprintf("rare-%d", i), 1)
	}
	drainAll(t, b)

	d := snapData(t, b.Snapshot())
	if got := find(d.Searches, "frequent"); got != 10 {
		t.Errorf("frequent count = %d, want 10: eviction dropped the frequent query", got)
	}
	if d.DroppedKeys != 1 {
		t.Errorf("dropped keys = %d, want 1", d.DroppedKeys)
	}
}

func drainAll(t *testing.T, b *usage.Bucket) {
	t.Helper()
	for range 100 {
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
