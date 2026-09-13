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

func TestEvictIdleUsers_ReclaimsIdleButKeepsActiveAndRecent(t *testing.T) {
	current := time.Unix(0, 0).UTC()
	bus := newBusWithClock(func() time.Time { return current })

	idle := shared.NewUserId(uuid.New())
	active := shared.NewUserId(uuid.New())
	bus.Publish(idle, "e", nil)
	_, cancelActive := bus.Subscribe(active)
	defer cancelActive()
	bus.Publish(active, "e", nil)

	current = current.Add(userIdleTTL + time.Minute)

	recent := shared.NewUserId(uuid.New())
	bus.Publish(recent, "e", nil)
	trigger := shared.NewUserId(uuid.New())
	bus.Publish(trigger, "e", nil)

	if _, ok := bus.users.Load(idle.String()); ok {
		t.Fatalf("idle subscriber-less user was not evicted")
	}
	if _, ok := bus.users.Load(active.String()); !ok {
		t.Fatalf("active subscribed user was wrongly evicted")
	}
	if _, ok := bus.users.Load(recent.String()); !ok {
		t.Fatalf("recently active subscriber-less user was wrongly evicted")
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

func TestReplay_AfterIdleEvictionAndRecreate_ReturnsNewEvents(t *testing.T) {
	current := time.Unix(0, 0).UTC()
	bus := newBusWithClock(func() time.Time { return current })
	user := shared.NewUserId(uuid.New())

	for i := 0; i < 5; i++ {
		bus.Publish(user, "before", nil)
	}
	var lastSeenID uint64
	for _, evt := range bus.Replay(user, 0) {
		lastSeenID = evt.ID
	}
	if lastSeenID == 0 {
		t.Fatalf("precondition: no pre-eviction events replayed")
	}

	current = current.Add(userIdleTTL + time.Minute)
	bus.Publish(shared.NewUserId(uuid.New()), "trigger", nil)
	if _, ok := bus.users.Load(user.String()); ok {
		t.Fatalf("precondition: idle user was not evicted")
	}

	bus.Publish(user, "after", nil)

	got := bus.Replay(user, lastSeenID)
	if len(got) != 1 || got[0].Type != "after" {
		t.Fatalf("replay after id %d = %+v, want the one post-eviction event", lastSeenID, got)
	}
	if got[0].ID <= lastSeenID {
		t.Fatalf("post-eviction event id %d reused an id at or below previously issued %d", got[0].ID, lastSeenID)
	}
}

type subscription struct {
	ch     <-chan Event
	cancel func()
}

func subscribeDuringEviction(bus *InProcessBus, user shared.UserId, out chan subscription) func(string) {
	return func(key string) {
		if key != user.String() {
			return
		}
		go func() {
			ch, cancel := bus.Subscribe(user)
			out <- subscription{ch, cancel}
		}()
		select {
		case sub := <-out:
			out <- sub
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestEvictIdleUsers_ConcurrentSubscribeIsNotOrphaned(t *testing.T) {
	current := time.Unix(0, 0).UTC()
	bus := newBusWithClock(func() time.Time { return current })
	idle := shared.NewUserId(uuid.New())
	bus.Publish(idle, "e", nil)
	current = current.Add(userIdleTTL + time.Minute)

	subscribed := make(chan subscription, 1)
	bus.beforeEvictDelete = subscribeDuringEviction(bus, idle, subscribed)
	bus.Publish(shared.NewUserId(uuid.New()), "trigger", nil)
	sub := <-subscribed
	defer sub.cancel()

	bus.Publish(idle, "after", nil)

	select {
	case evt := <-sub.ch:
		if evt.Type != "after" {
			t.Fatalf("subscriber got event %q, want %q", evt.Type, "after")
		}
	case <-time.After(time.Second):
		t.Fatalf("subscriber registered during eviction was orphaned: no event delivered")
	}
}
