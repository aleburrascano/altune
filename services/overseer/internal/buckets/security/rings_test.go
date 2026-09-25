package security_test

import (
	"altune/overseer/internal/buckets/security"
	"altune/overseer/internal/core"
	"encoding/json"
	"testing"
	"time"
)

func TestARestoredHistoryRingShowsInTheSnapshot(t *testing.T) {
	b := security.New()
	restored := []core.Signal{{At: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), Kind: "suite", Text: "4/4 passed"}}

	ring := b.Rings()["history"]
	if ring == nil {
		t.Fatal("Rings() has no history ring")
	}

	ring.Restore(restored)

	var data security.Data
	if err := json.Unmarshal(b.Snapshot().Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(data.History) != 1 || data.History[0].Text != "4/4 passed" {
		t.Fatalf("snapshot history = %+v, want the restored %+v", data.History, restored)
	}
}
