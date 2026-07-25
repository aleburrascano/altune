package events

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"altune/go-api/internal/shared"
)

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

func TestPublish_LaterProcessHasHigherIDs(t *testing.T) {
	user := shared.NewUserId(uuid.New())

	bus1 := NewInProcessBus()
	ch1, cancel1 := bus1.Subscribe(user)
	defer cancel1()
	bus1.Publish(user, "e", nil)
	id1 := (<-ch1).ID

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
