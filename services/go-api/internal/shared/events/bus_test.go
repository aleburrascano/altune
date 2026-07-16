package events

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"altune/go-api/internal/shared"
)

// TestPublish_EpochSeedsEventIDs is the F5 regression: event ids must be seeded
// from a per-process epoch, not reset to a low value each process start. If ids
// restarted near 1, a client that had already seen id 1 from a previous process
// would 204/mis-dedupe on reconnect after a server restart.
func TestPublish_EpochSeedsEventIDs(t *testing.T) {
	bus := NewInProcessBus()
	user := shared.NewUserId(uuid.New())

	ch, cancel := bus.Subscribe(user)
	defer cancel()

	bus.Publish(user, "first", map[string]any{"k": "v"})
	evt := <-ch

	if evt.ID <= 1 {
		t.Fatalf("first event id = %d, want an epoch-seeded id well above 1", evt.ID)
	}
}

// TestPublish_LaterProcessHasHigherIDs asserts a bus constructed later assigns a
// strictly higher first id than one constructed earlier — the monotonic-across-
// restart property F5 provides.
func TestPublish_LaterProcessHasHigherIDs(t *testing.T) {
	user := shared.NewUserId(uuid.New())

	bus1 := NewInProcessBus()
	ch1, cancel1 := bus1.Subscribe(user)
	defer cancel1()
	bus1.Publish(user, "e", nil)
	id1 := (<-ch1).ID

	// The F5 property is about process *restarts* (seconds apart). Back-to-back
	// constructions can land in the same clock tick on a coarse timer, so step
	// the wall clock past the earlier seed before constructing the "later
	// process" bus.
	time.Sleep(time.Millisecond)

	bus2 := NewInProcessBus()
	ch2, cancel2 := bus2.Subscribe(user)
	defer cancel2()
	bus2.Publish(user, "e", nil)
	id2 := (<-ch2).ID

	if id2 <= id1 {
		t.Fatalf("later bus first id = %d, want > earlier bus first id %d", id2, id1)
	}
}
