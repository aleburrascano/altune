package events_test

import (
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/events/eventstest"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/timingtest"
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

const (
	ringSize           = 100
	subscriberChanSize = 16
	userIdleTTL        = 30 * time.Minute
)

func testFloorPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "altune-event-id-floor")
}

type testClock struct{ nanos atomic.Int64 }

func newTestClock(at time.Time) *testClock {
	c := &testClock{}
	c.nanos.Store(at.UnixNano())
	return c
}

func (c *testClock) now() time.Time { return time.Unix(0, c.nanos.Load()).UTC() }

func (c *testClock) advance(d time.Duration) { c.nanos.Add(int64(d)) }

func busAt(t *testing.T, clock *testClock) *events.InProcessBus {
	t.Helper()
	return events.NewInProcessBus(context.Background(), clock.now, testFloorPath(t))
}

func newUser() ids.UserId { return ids.NewUserId(uuid.New()) }

func evictIdleUsers(bus *events.InProcessBus, clock *testClock) {
	clock.advance(userIdleTTL + time.Minute)
	bus.Publish(context.Background(), newUser(), eventstest.Named("trigger"))
}

func isTracked(bus *events.InProcessBus, user ids.UserId) bool {
	return bus.LatestID(user) != 0
}

func TestPublish_EpochSeedsEventIDs(t *testing.T) {
	bus := events.NewInProcessBus(context.Background(), time.Now, testFloorPath(t))
	user := newUser()

	ch, cancel := bus.Subscribe(user)
	defer cancel()

	bus.Publish(context.Background(), user, eventstest.Fielded("first", map[string]any{"k": "v"}))
	evt := <-ch

	if evt.ID <= 1 {
		t.Fatalf("first event id = %d, want an epoch-seeded id well above 1", evt.ID)
	}
	if evt.Type != "first" || evt.UserID != user || evt.Payload.(eventstest.Event).Fields["k"] != "v" || evt.Timestamp.IsZero() {
		t.Fatalf("event = %+v, want the published type, user, payload and a timestamp", evt)
	}
	if got := bus.HighestIssuedID(); got != evt.ID {
		t.Fatalf("HighestIssuedID = %d, want the last issued id %d", got, evt.ID)
	}
}

func TestEvictIdleUsers_ReclaimsIdleButKeepsActiveAndRecent(t *testing.T) {
	clock := newTestClock(time.Unix(0, 0))
	bus := busAt(t, clock)

	idle := newUser()
	active := newUser()
	bus.Publish(context.Background(), idle, eventstest.Named("e"))
	_, cancelActive := bus.Subscribe(active)
	defer cancelActive()
	bus.Publish(context.Background(), active, eventstest.Named("e"))

	clock.advance(userIdleTTL + time.Minute)

	recent := newUser()
	bus.Publish(context.Background(), recent, eventstest.Named("e"))
	bus.Publish(context.Background(), newUser(), eventstest.Named("e"))

	if isTracked(bus, idle) {
		t.Fatalf("idle subscriber-less user was not evicted")
	}
	if !isTracked(bus, active) {
		t.Fatalf("active subscribed user was wrongly evicted")
	}
	if !isTracked(bus, recent) {
		t.Fatalf("recently active subscriber-less user was wrongly evicted")
	}
}

func TestEvictIdleUsers_KeepsAUserIdleForExactlyTheTTL(t *testing.T) {
	clock := newTestClock(time.Unix(0, 0))
	bus := busAt(t, clock)
	user := newUser()
	bus.Publish(context.Background(), user, eventstest.Named("e"))

	clock.advance(userIdleTTL)
	bus.Publish(context.Background(), newUser(), eventstest.Named("trigger"))

	if !isTracked(bus, user) {
		t.Fatalf("user idle for exactly %s was evicted, want eviction only past it", userIdleTTL)
	}
}

func TestEvictIdleUsers_CancelledSubscriptionStartsTheIdleClock(t *testing.T) {
	clock := newTestClock(time.Unix(0, 0))
	bus := busAt(t, clock)
	user := newUser()
	_, cancel := bus.Subscribe(user)
	bus.Publish(context.Background(), user, eventstest.Named("e"))
	clock.advance(userIdleTTL + time.Minute)
	cancel()

	bus.Publish(context.Background(), newUser(), eventstest.Named("trigger"))
	if !isTracked(bus, user) {
		t.Fatal("user evicted right after its last subscriber left, want the idle TTL to start then")
	}
	evictIdleUsers(bus, clock)
	if isTracked(bus, user) {
		t.Fatal("user still tracked a full idle TTL after its last subscriber left")
	}
}

func TestPublish_LaterProcessHasHigherIDs(t *testing.T) {
	user := newUser()
	floorPath := testFloorPath(t)

	frozen := func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }

	bus1 := events.NewInProcessBus(context.Background(), frozen, floorPath)
	ch1, cancel1 := bus1.Subscribe(user)
	defer cancel1()
	bus1.Publish(context.Background(), user, eventstest.Named("e"))
	id1 := (<-ch1).ID

	bus2 := events.NewInProcessBus(context.Background(), frozen, floorPath)
	ch2, cancel2 := bus2.Subscribe(user)
	defer cancel2()
	bus2.Publish(context.Background(), user, eventstest.Named("e"))
	id2 := (<-ch2).ID

	if id2 <= id1 {
		t.Fatalf("later bus first id = %d, want > earlier bus first id %d", id2, id1)
	}
}

func restartAt(at time.Time, floorPath string) *events.InProcessBus {
	return events.NewInProcessBus(context.Background(), func() time.Time { return at }, floorPath)
}

func TestReplay_AfterBackwardClockJumpAcrossRestart_SeesNewEvents(t *testing.T) {
	floorPath := testFloorPath(t)
	user := newUser()
	beforeRestart := time.Unix(1_700_000_000, 0).UTC()

	old := restartAt(beforeRestart, floorPath)
	old.Publish(context.Background(), user, eventstest.Named("before"))
	clientAfterID := old.Replay(context.Background(), user, 0)[0].ID

	restarted := restartAt(beforeRestart.Add(-time.Hour), floorPath)
	restarted.Publish(context.Background(), user, eventstest.Named("after"))

	got := restarted.Replay(context.Background(), user, clientAfterID)
	if len(got) != 1 || got[0].Type != "after" {
		t.Fatalf("replay after id %d = %+v, want the one event published since the restart", clientAfterID, got)
	}
}

func TestPublish_AcrossRepeatedBackwardClockJumps_NeverReissuesAnID(t *testing.T) {
	floorPath := testFloorPath(t)
	user := newUser()
	clock := time.Unix(1_700_000_000, 0).UTC()

	var highest uint64
	for restart := 0; restart < 5; restart++ {
		bus := restartAt(clock, floorPath)
		bus.Publish(context.Background(), user, eventstest.Named("e"))
		id := bus.Replay(context.Background(), user, 0)[0].ID
		if id <= highest {
			t.Fatalf("restart %d issued id %d, at or below the already-issued %d", restart, id, highest)
		}
		highest = id
		clock = clock.Add(-24 * time.Hour)
	}
}

func TestReplay_AfterIdleEvictionAndRecreate_ReturnsNewEvents(t *testing.T) {
	clock := newTestClock(time.Unix(0, 0))
	bus := busAt(t, clock)
	user := newUser()

	for i := 0; i < 5; i++ {
		bus.Publish(context.Background(), user, eventstest.Named("before"))
	}
	var lastSeenID uint64
	for _, evt := range bus.Replay(context.Background(), user, 0) {
		lastSeenID = evt.ID
	}
	if lastSeenID == 0 {
		t.Fatalf("precondition: no pre-eviction events replayed")
	}

	evictIdleUsers(bus, clock)
	if isTracked(bus, user) {
		t.Fatalf("precondition: idle user was not evicted")
	}
	if got := bus.Replay(context.Background(), user, 0); got != nil {
		t.Fatalf("replay of an evicted user = %+v, want nothing", got)
	}

	bus.Publish(context.Background(), user, eventstest.Named("after"))

	got := bus.Replay(context.Background(), user, lastSeenID)
	if len(got) != 1 || got[0].Type != "after" {
		t.Fatalf("replay after id %d = %+v, want the one post-eviction event", lastSeenID, got)
	}
	if got[0].ID <= lastSeenID {
		t.Fatalf("post-eviction event id %d reused an id at or below previously issued %d", got[0].ID, lastSeenID)
	}
}

func TestReplay_ReturnsOnlyEventsAfterTheGivenID(t *testing.T) {
	bus := busAt(t, newTestClock(time.Unix(1_700_000_000, 0)))
	user := newUser()
	for range 3 {
		bus.Publish(context.Background(), user, eventstest.Named("e"))
	}
	all := bus.Replay(context.Background(), user, 0)

	got := bus.Replay(context.Background(), user, all[0].ID)

	if len(got) != 2 || got[0].ID != all[1].ID || got[1].ID != all[2].ID {
		t.Fatalf("replay after id %d = %+v, want the two later events", all[0].ID, got)
	}
}

func TestReplay_FullRingKeepsTheNewestEventsInOrder(t *testing.T) {
	bus := busAt(t, newTestClock(time.Unix(1_700_000_000, 0)))
	user := newUser()
	for range ringSize + 2 {
		bus.Publish(context.Background(), user, eventstest.Named("e"))
	}

	got := bus.Replay(context.Background(), user, 0)

	if len(got) != ringSize {
		t.Fatalf("replay returned %d events, want the ring's %d", len(got), ringSize)
	}
	for i := 1; i < len(got); i++ {
		if got[i].ID != got[i-1].ID+1 {
			t.Fatalf("replay ids %d then %d, want consecutive ids oldest first", got[i-1].ID, got[i].ID)
		}
	}
	if got[len(got)-1].ID != bus.LatestID(user) {
		t.Fatalf("replay ends at %d, want the latest id %d", got[len(got)-1].ID, bus.LatestID(user))
	}
}

func TestReplay_UnknownUserReturnsNothing(t *testing.T) {
	bus := busAt(t, newTestClock(time.Unix(1_700_000_000, 0)))
	user := newUser()
	_, cancel := bus.Subscribe(user)
	defer cancel()

	if got := bus.Replay(context.Background(), newUser(), 0); got != nil {
		t.Fatalf("replay of an unknown user = %+v, want nothing", got)
	}
	if got := bus.Replay(context.Background(), user, 0); got != nil {
		t.Fatalf("replay of a user with no events = %+v, want nothing", got)
	}
}

type warnRecorder struct {
	mu    sync.Mutex
	warns []map[string]any
}

func (h *warnRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (h *warnRecorder) Handle(_ context.Context, r slog.Record) error {
	if r.Level != slog.LevelWarn {
		return nil
	}
	attrs := map[string]any{"msg": r.Message}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.warns = append(h.warns, attrs)
	return nil
}

func (h *warnRecorder) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *warnRecorder) WithGroup(string) slog.Handler { return h }

func (h *warnRecorder) named(msg string) []map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []map[string]any
	for _, w := range h.warns {
		if w["msg"] == msg {
			out = append(out, w)
		}
	}
	return out
}

func captureWarnings(t *testing.T) *warnRecorder {
	t.Helper()
	rec := &warnRecorder{}
	logging.UseHandler(t, rec)
	return rec
}

func TestReplay_WarnsWithTheLostCountOnlyWhenTheRingDroppedEventsTheClientNeeds(t *testing.T) {
	bus := busAt(t, newTestClock(time.Unix(1_700_000_000, 0)))
	user := newUser()
	var first uint64
	for i := range ringSize + 2 {
		bus.Publish(context.Background(), user, eventstest.Named("e"))
		if i == 0 {
			first = bus.LatestID(user)
		}
	}
	warnings := captureWarnings(t)

	bus.Replay(context.Background(), user, 0)
	bus.Replay(context.Background(), user, first+1)
	if got := warnings.named("events.replay_gap"); len(got) != 0 {
		t.Fatalf("warnings = %v, want none for a fresh start or a client that missed nothing", got)
	}

	bus.Replay(context.Background(), user, first)

	got := warnings.named("events.replay_gap")
	if len(got) != 1 {
		t.Fatalf("warnings = %v, want one replay gap", warnings.warns)
	}
	if got[0]["after_id"] != first || got[0]["oldest_retained_id"] != first+2 || got[0]["lost"] != uint64(1) {
		t.Fatalf("gap warning = %v, want after_id=%d oldest_retained_id=%d lost=1", got[0], first, first+2)
	}
}

type subscription struct {
	ch     <-chan events.Event
	cancel func()
}

func TestPublish_ConcurrentSameUserDeliversIDsInOrder(t *testing.T) {
	const publishers = 4
	const perPublisher = subscriberChanSize / publishers
	const rounds = 300
	bus := events.NewInProcessBus(context.Background(), time.Now, testFloorPath(t))
	user := newUser()
	ch, cancel := bus.Subscribe(user)
	defer cancel()

	var last uint64
	for range rounds {
		var wg sync.WaitGroup
		for range publishers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range perPublisher {
					bus.Publish(context.Background(), user, eventstest.Named("e"))
				}
			}()
		}
		wg.Wait()

		for range publishers * perPublisher {
			evt := <-ch
			if evt.ID <= last {
				t.Fatalf("received id %d after %d, want strictly increasing", evt.ID, last)
			}
			last = evt.ID
		}
	}
}

type clockGate struct {
	entered chan struct{}
	release chan struct{}
	returns time.Time
}

type gatedClock struct {
	mu   sync.Mutex
	at   time.Time
	gate *clockGate
}

func (c *gatedClock) now() time.Time {
	c.mu.Lock()
	gate, at := c.gate, c.at
	c.gate = nil
	c.mu.Unlock()
	if gate == nil {
		return at
	}
	close(gate.entered)
	<-gate.release
	return gate.returns
}

func (c *gatedClock) gateNextCall(returns time.Time, release chan struct{}) *clockGate {
	gate := &clockGate{entered: make(chan struct{}), release: release, returns: returns}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gate = gate
	return gate
}

func (c *gatedClock) current() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *gatedClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

func holdUserLockWithStaleActivity(bus *events.InProcessBus, clock *gatedClock, user ids.UserId, staleAt time.Time) chan struct{} {
	release := make(chan struct{})
	held := clock.gateNextCall(staleAt, release)
	go bus.Publish(context.Background(), user, eventstest.Named("held"))
	<-held.entered
	return release
}

func awaitQueuedOnLock(t *testing.T, function string) {
	t.Helper()
	timingtest.Until(t, 5*time.Second, "a goroutine in "+function+" to queue on the user lock", func() bool {
		buf := make([]byte, 1<<20)
		dump := string(buf[:runtime.Stack(buf, true)])
		for _, g := range strings.Split(dump, "\n\n") {
			header, _, _ := strings.Cut(g, "\n")
			queued := strings.Contains(header, "Mutex.Lock") || strings.Contains(header, "semacquire")
			if queued && strings.Contains(g, function) {
				return true
			}
		}
		return false
	})
}

func startEvictionQueuedOnUserLock(t *testing.T, bus *events.InProcessBus, clock *gatedClock) chan struct{} {
	done := make(chan struct{})
	released := make(chan struct{})
	close(released)
	cutoffRead := clock.gateNextCall(clock.current(), released)
	go func() {
		defer close(done)
		bus.Publish(context.Background(), newUser(), eventstest.Named("trigger"))
	}()
	<-cutoffRead.entered
	awaitQueuedOnLock(t, "(*userState).evictIfReclaimable")
	return done
}

func TestEvictIdleUsers_SubscribeQueuedBehindTheEvictionIsNotOrphaned(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC()
	clock := &gatedClock{at: start}
	bus := events.NewInProcessBus(context.Background(), clock.now, testFloorPath(t))
	idle := newUser()
	bus.Publish(context.Background(), idle, eventstest.Named("before"))
	clock.advance(userIdleTTL + time.Minute)

	releaseUser := holdUserLockWithStaleActivity(bus, clock, idle, start)
	evicted := startEvictionQueuedOnUserLock(t, bus, clock)
	subscribed := make(chan subscription, 1)
	go func() {
		ch, cancel := bus.Subscribe(idle)
		subscribed <- subscription{ch, cancel}
	}()
	awaitQueuedOnLock(t, "(*InProcessBus).Subscribe")
	close(releaseUser)
	<-evicted
	sub := <-subscribed
	defer sub.cancel()

	bus.Publish(context.Background(), idle, eventstest.Named("after"))

	select {
	case evt := <-sub.ch:
		if evt.Type != "after" {
			t.Fatalf("subscriber got event %q, want %q", evt.Type, "after")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber that queued behind the eviction was orphaned: the next event never reached it")
	}
}

func TestLatestID_TracksLastPublishedIDEvenWhenSubscriberDropped(t *testing.T) {
	bus := events.NewInProcessBus(context.Background(), time.Now, testFloorPath(t))
	uid := newUser()
	if got := bus.LatestID(uid); got != 0 {
		t.Fatalf("LatestID for unknown user = %d, want 0", got)
	}
	_, cancel := bus.Subscribe(uid)
	defer cancel()
	for i := 0; i < subscriberChanSize+4; i++ {
		bus.Publish(context.Background(), uid, eventstest.Named("e"))
	}
	replayed := bus.Replay(context.Background(), uid, 0)
	if want := replayed[len(replayed)-1].ID; bus.LatestID(uid) != want {
		t.Fatalf("LatestID = %d, want %d", bus.LatestID(uid), want)
	}
	if got := bus.Dropped(); got != 4 {
		t.Fatalf("Dropped = %d, want the 4 events past the subscriber's %d-slot buffer", got, subscriberChanSize)
	}
}

func TestResumeGapped_AfterIdleEvictionWithNothingPublished_ReportsGap(t *testing.T) {
	clock := newTestClock(time.Unix(0, 0))
	bus := busAt(t, clock)
	user := newUser()
	bus.Publish(context.Background(), user, eventstest.Named("before"))
	lastSeenID := bus.Replay(context.Background(), user, 0)[0].ID

	evictIdleUsers(bus, clock)
	if isTracked(bus, user) {
		t.Fatalf("precondition: idle user was not evicted")
	}

	if !bus.ResumeGapped(user, lastSeenID) {
		t.Fatalf("ResumeGapped(after %d) = false after eviction, want true", lastSeenID)
	}
	_, cancel := bus.Subscribe(user)
	defer cancel()
	if !bus.ResumeGapped(user, lastSeenID) {
		t.Fatalf("ResumeGapped(after %d) = false on the recreated state, want true", lastSeenID)
	}
}

func TestResumeGapped_ClientAtTheHighestIDWhenStateWasRecreated_ReportsNoGap(t *testing.T) {
	clock := newTestClock(time.Unix(0, 0))
	bus := busAt(t, clock)
	user := newUser()
	bus.Publish(context.Background(), user, eventstest.Named("before"))
	lastSeenID := bus.LatestID(user)
	clock.advance(userIdleTTL + time.Minute)
	_, cancelTrigger := bus.Subscribe(newUser())
	defer cancelTrigger()
	if isTracked(bus, user) {
		t.Fatalf("precondition: idle user was not evicted")
	}

	_, cancel := bus.Subscribe(user)
	defer cancel()

	if bus.ResumeGapped(user, lastSeenID) {
		t.Fatalf("ResumeGapped(after %d) = true, want false: no id was issued while the user's state was gone", lastSeenID)
	}
}

func TestResumeGapped_AfterRestart_ReportsGap(t *testing.T) {
	floorPath := testFloorPath(t)
	user := newUser()
	old := restartAt(time.Unix(1_700_000_000, 0).UTC(), floorPath)
	old.Publish(context.Background(), user, eventstest.Named("before"))
	lastSeenID := old.Replay(context.Background(), user, 0)[0].ID

	restarted := restartAt(time.Unix(1_700_000_000, 0).UTC(), floorPath)
	if !restarted.ResumeGapped(user, lastSeenID) {
		t.Fatalf("ResumeGapped(after %d) = false after restart, want true", lastSeenID)
	}
}

func TestResumeGapped_CaughtUpOrFresh_ReportsNoGap(t *testing.T) {
	bus := events.NewInProcessBus(context.Background(), time.Now, testFloorPath(t))
	user := newUser()
	bus.Publish(context.Background(), user, eventstest.Named("e"))
	lastSeenID := bus.Replay(context.Background(), user, 0)[0].ID

	if bus.ResumeGapped(user, lastSeenID) {
		t.Fatalf("ResumeGapped(after %d) = true for a caught-up client", lastSeenID)
	}
	if bus.ResumeGapped(newUser(), 0) {
		t.Fatalf("ResumeGapped(after 0) = true, want false: nothing was ever seen")
	}
}
