package liveactivity

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSource is a controllable stand-in for the SSE consumer: a test pushes
// events onto it and flips its status, so the collect/snapshot/degrade paths are
// exercised deterministically with no network and no wall-clock timing.
type fakeSource struct {
	events chan goapi.Event
	status atomic.Int32
}

func newFakeSource(buf int) *fakeSource {
	f := &fakeSource{events: make(chan goapi.Event, buf)}
	f.status.Store(int32(goapi.StatusUp))
	return f
}

func (f *fakeSource) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (f *fakeSource) Events() <-chan goapi.Event    { return f.events }
func (f *fakeSource) Status() goapi.Status          { return goapi.Status(f.status.Load()) }
func (f *fakeSource) setStatus(s goapi.Status)      { f.status.Store(int32(s)) }
func (f *fakeSource) push(ev goapi.Event)           { f.events <- ev }

// collectStore runs one collect/store cycle, the pair the shell drives on a tick.
func collectStore(t *testing.T, b *Bucket) {
	t.Helper()
	signals, err := b.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	b.Store(signals)
}

// snapData unmarshals a snapshot's Data into the bucket's typed payload.
func snapData(t *testing.T, snap core.Snapshot) Data {
	t.Helper()
	var d Data
	if err := json.Unmarshal(snap.Data, &d); err != nil {
		t.Fatalf("unmarshal data: %v (%s)", err, snap.Data)
	}
	return d
}

// TestCollectStoreSnapshotLiveFeed is the core Done proof: events flow from the
// source into the bounded ring and appear in the snapshot as a live feed, with the
// raw (unescaped) watched-app text carried in the JSON payload — escaping is the
// frontend's job now, so the API must carry the value verbatim.
func TestCollectStoreSnapshotLiveFeed(t *testing.T) {
	src := newFakeSource(8)
	b := newBucket(src)

	empty := b.Snapshot()
	if empty.State != core.StateLive {
		t.Errorf("empty state = %q, want live", empty.State)
	}
	if got := len(snapData(t, empty).Events); got != 0 {
		t.Errorf("empty events = %d, want 0", got)
	}

	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	src.push(goapi.Event{Type: "track.played", Timestamp: when, User: "u1", Subject: "song a"})
	src.push(goapi.Event{Type: "search.performed", Subject: "<script>jazz"})
	collectStore(t, b)

	snap := b.Snapshot()
	if snap.State != core.StateLive {
		t.Errorf("state = %q, want live", snap.State)
	}
	d := snapData(t, snap)
	if len(d.Events) != 2 {
		t.Fatalf("events = %d, want 2", len(d.Events))
	}
	if d.InFlightAvailable {
		t.Error("inFlightAvailable = true, want false (no go-api in-flight read yet)")
	}
	// The raw, unescaped watched-app text must be carried verbatim; the frontend
	// (React) escapes it on render.
	if d.Events[1].Text != "search.performed <script>jazz" {
		t.Errorf("event text = %q, want raw unescaped subject", d.Events[1].Text)
	}
	if b.Meta().ID != "liveactivity" {
		t.Errorf("Meta.ID = %q, want liveactivity", b.Meta().ID)
	}
}

// TestDegradesToSourceDownWhenSourceDown is the spine proof: drop the source and
// the snapshot flips to source_down while still carrying the last-known feed, so
// the panel never goes blank.
func TestDegradesToSourceDownWhenSourceDown(t *testing.T) {
	src := newFakeSource(8)
	b := newBucket(src)
	src.push(goapi.Event{Type: "track.played", Subject: "last known song"})
	collectStore(t, b)

	if snap := b.Snapshot(); snap.State != core.StateLive {
		t.Fatalf("pre-drop state = %q, want live", snap.State)
	}

	src.setStatus(goapi.StatusDown) // the source drops

	snap := b.Snapshot()
	if snap.State != core.StateSourceDown {
		t.Errorf("post-drop state = %q, want source_down", snap.State)
	}
	d := snapData(t, snap)
	if len(d.Events) != 1 || d.Events[0].Text != "track.played last known song" {
		t.Errorf("post-drop lost last-known feed: %+v", d.Events)
	}
}

// TestHeadlineSurvivesTheSourceGoingDown proves the health half is read off the
// payload, not off freshness: the source drops, State flips to source_down, and
// the headline still reports the feed the bucket is serving. Live activity grades
// ok by construction — a domain-event feed carries what happened, not what failed.
func TestHeadlineSurvivesTheSourceGoingDown(t *testing.T) {
	src := newFakeSource(8)
	b := newBucket(src)
	src.push(goapi.Event{Type: "track.played", Subject: "last known song"})
	collectStore(t, b)
	if got := b.Snapshot().Headline; got != "1 events" {
		t.Fatalf("live headline = %q, want the feed depth", got)
	}

	src.setStatus(goapi.StatusDown)
	snap := b.Snapshot()

	if snap.State != core.StateSourceDown {
		t.Fatalf("state = %q, want source_down", snap.State)
	}
	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok — a dropped stream is freshness, not health", snap.Severity)
	}
	if snap.Headline != "1 events" {
		t.Errorf("headline = %q, want the last-known feed depth", snap.Headline)
	}
}

// TestConnectingIsStale proves the third state: a source that is connecting (not
// yet up, not down) reports stale.
func TestConnectingIsStale(t *testing.T) {
	src := newFakeSource(1)
	src.setStatus(goapi.StatusConnecting)
	b := newBucket(src)
	if snap := b.Snapshot(); snap.State != core.StateStale {
		t.Errorf("connecting state = %q, want stale", snap.State)
	}
}

// TestCollectReportsSourceDownWithNoFreshEvents proves Collect follows the Bucket
// contract: an unreachable source with nothing fresh returns an error (so the
// shell keeps last-known state), while a healthy source returns no error.
func TestCollectReportsSourceDownWithNoFreshEvents(t *testing.T) {
	src := newFakeSource(1)
	b := newBucket(src)

	src.setStatus(goapi.StatusDown)
	if _, err := b.Collect(context.Background()); err == nil {
		t.Error("Collect returned nil while source down with no events, want an error")
	}

	src.setStatus(goapi.StatusUp)
	if _, err := b.Collect(context.Background()); err != nil {
		t.Errorf("Collect while up = %v, want nil", err)
	}
}

// TestStaysBoundedUnderLoad is the bounded-storage proof: feeding many times the
// ring capacity never grows retained storage past the cap.
func TestStaysBoundedUnderLoad(t *testing.T) {
	src := newFakeSource(4 * eventCapacity)
	b := newBucket(src)
	for i := 0; i < 4*eventCapacity; i++ {
		src.push(goapi.Event{Type: "flood", Subject: "e"})
	}
	for b.events.Len() < eventCapacity {
		collectStore(t, b)
	}
	collectStore(t, b)

	if got := b.events.Len(); got != eventCapacity {
		t.Errorf("retained %d signals, want capped at %d", got, eventCapacity)
	}
	if got := len(snapData(t, b.Snapshot()).Events); got != eventCapacity {
		t.Errorf("snapshot feed = %d, want capped at %d", got, eventCapacity)
	}
}

// TestConcurrentCollectAndSnapshot is the concurrency attack: the collect/store
// cycle and Snapshot run together (as the tick loop and HTTP handlers do) with no
// data race — run under -race.
func TestConcurrentCollectAndSnapshot(t *testing.T) {
	src := newFakeSource(256)
	b := newBucket(src)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			select {
			case src.events <- goapi.Event{Type: "e", Subject: "x"}:
			default:
			}
			signals, _ := b.Collect(ctx)
			b.Store(signals)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = b.Snapshot()
		}
	}()
	wg.Wait()
}

// TestUnconfiguredDegradesNotCrashes proves the production constructor with no
// go-api env yields a bucket that reports source_down and stays bounded, never
// panicking.
func TestUnconfiguredDegradesNotCrashes(t *testing.T) {
	t.Setenv("OVERSEER_GOAPI_URL", "")
	t.Setenv("OVERSEER_GOAPI_TOKEN", "")
	b := New()
	if _, ok := b.src.(*nullSource); !ok {
		t.Fatalf("unconfigured source = %T, want *nullSource", b.src)
	}
	if snap := b.Snapshot(); snap.State != core.StateSourceDown {
		t.Errorf("unconfigured state = %q, want source_down", snap.State)
	}
}
