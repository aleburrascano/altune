package usage

import (
	"altune/overseer/internal/goapi"
	"testing"
)

// TestSearchRollupCountsMaskedSearches pins #2594: go-api's admin stream masks
// search text (#2585), so a real search_performed signal ordinarily carries no
// Subject. The usage bucket must still show the search count moving rather
// than silently dropping the event as an empty key.
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
