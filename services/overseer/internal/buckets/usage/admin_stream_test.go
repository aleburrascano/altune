package usage

import (
	"altune/overseer/internal/goapi"
	"testing"
)

func TestSearchRollupCountsMaskedSearches(t *testing.T) {
	src := newFakeSource(4)
	b := newBucket(src)

	src.push(goapi.Event{Type: "search_performed"})
	src.push(goapi.Event{Type: "search_performed"})
	src.push(goapi.Event{Type: "play"})
	drainAll(t, b)

	data := snapData(t, b.Snapshot())
	if got := find(data.Searches, "search_performed"); got != 2 {
		t.Errorf("masked search count = %d, want 2", got)
	}
	if got := find(data.Plays, "play"); got != 1 {
		t.Errorf("play count = %d, want 1", got)
	}
}
