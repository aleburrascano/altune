package reliability_test

import (
	"altune/overseer/internal/buckets/reliability"
	"altune/overseer/internal/core"
	"encoding/json"
	"testing"
	"time"
)

func ringNamed(t *testing.T, b core.Restorable, name string) *core.RingStore {
	t.Helper()
	ring := b.Rings()[name]
	if ring == nil {
		t.Fatalf("Rings() has no %q ring", name)
	}
	return ring
}

func TestRestoredHistoryAndPollRingsShowInTheSnapshot(t *testing.T) {
	b := reliability.New()
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	history := []core.Signal{{At: at, Kind: "health", Text: "db=up redis=up auth=up (healthy)"}}
	poll := []core.Signal{{At: at, Kind: "poll", Text: "up"}, {At: at.Add(time.Minute), Kind: "poll", Text: "down"}}
	historyRing, pollRing := ringNamed(t, b, "history"), ringNamed(t, b, "poll")

	historyRing.Restore(history)
	pollRing.Restore(poll)

	var data reliability.Data
	if err := json.Unmarshal(b.Snapshot().Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(data.History) != 1 || data.History[0].Text != history[0].Text {
		t.Fatalf("snapshot history = %+v, want the restored %+v", data.History, history)
	}
	if len(data.Poll) != 2 || data.Poll[0].Text != "up" || data.Poll[1].Text != "down" {
		t.Fatalf("snapshot poll = %+v, want the restored %+v", data.Poll, poll)
	}
}
