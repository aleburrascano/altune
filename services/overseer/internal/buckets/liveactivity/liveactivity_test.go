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

func collectStore(t *testing.T, b *Bucket) {
	t.Helper()
	signals, err := b.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	b.Store(signals)
}

func snapData(t *testing.T, snap core.Snapshot) Data {
	t.Helper()
	var d Data
	if err := json.Unmarshal(snap.Data, &d); err != nil {
		t.Fatalf("unmarshal data: %v (%s)", err, snap.Data)
	}
	return d
}

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
	if d.Events[1].Text != "search.performed <script>jazz" {
		t.Errorf("event text = %q, want raw unescaped subject", d.Events[1].Text)
	}
	if b.Meta().ID != "liveactivity" {
		t.Errorf("Meta.ID = %q, want liveactivity", b.Meta().ID)
	}
}

func TestEventCorrelationIDPropagatesToSignal(t *testing.T) {
	src := newFakeSource(4)
	b := newBucket(src)

	src.push(goapi.Event{Type: "track.played", Subject: "song a", CorrID: "a1b2c3d4"})
	collectStore(t, b)

	d := snapData(t, b.Snapshot())
	if len(d.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(d.Events))
	}
	if d.Events[0].CorrID != "a1b2c3d4" {
		t.Fatalf("signal CorrID = %q, want a1b2c3d4", d.Events[0].CorrID)
	}
}

func TestDegradesToSourceDownWhenSourceDown(t *testing.T) {
	src := newFakeSource(8)
	b := newBucket(src)
	src.push(goapi.Event{Type: "track.played", Subject: "last known song"})
	collectStore(t, b)

	if snap := b.Snapshot(); snap.State != core.StateLive {
		t.Fatalf("pre-drop state = %q, want live", snap.State)
	}

	src.setStatus(goapi.StatusDown)

	snap := b.Snapshot()
	if snap.State != core.StateSourceDown {
		t.Errorf("post-drop state = %q, want source_down", snap.State)
	}
	d := snapData(t, snap)
	if len(d.Events) != 1 || d.Events[0].Text != "track.played last known song" {
		t.Errorf("post-drop lost last-known feed: %+v", d.Events)
	}
}

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

func TestConnectingIsStale(t *testing.T) {
	src := newFakeSource(1)
	src.setStatus(goapi.StatusConnecting)
	b := newBucket(src)
	if snap := b.Snapshot(); snap.State != core.StateStale {
		t.Errorf("connecting state = %q, want stale", snap.State)
	}
}

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

func TestSnapshotSurfacesDroppedCount(t *testing.T) {
	const overflow = 30
	src := newFakeSource(eventCapacity + overflow)
	b := newBucket(src)
	for i := 0; i < eventCapacity+overflow; i++ {
		src.push(goapi.Event{Type: "flood", Subject: "e"})
	}
	collectStore(t, b)

	if got := snapData(t, b.Snapshot()).Dropped; got != overflow {
		t.Fatalf("snapshot dropped = %d, want %d (events beyond cap)", got, overflow)
	}
}

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

type pumpSource struct {
	events  chan goapi.Event
	stopped chan struct{}
}

func newPumpSource() *pumpSource {
	return &pumpSource{events: make(chan goapi.Event, 64), stopped: make(chan struct{})}
}

func (p *pumpSource) Run(ctx context.Context) error {
	defer close(p.stopped)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			select {
			case p.events <- goapi.Event{Type: "tick", Subject: "e"}:
			default:
			}
		}
	}
}

func (p *pumpSource) Events() <-chan goapi.Event { return p.events }
func (p *pumpSource) Status() goapi.Status       { return goapi.StatusUp }

func TestStartKeepsPumpFeedingPastFirstTick(t *testing.T) {
	src := newPumpSource()
	b := newBucket(src)
	appCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	b.Start(appCtx)

	firstCtx, firstCancel := context.WithCancel(context.Background())
	first, _ := b.Collect(firstCtx)
	b.Store(first)
	firstCancel()

	seen := len(first)
	deadline := time.After(2 * time.Second)
	for seen < len(first)+eventCapacity {
		select {
		case <-deadline:
			t.Fatalf("pump delivered %d events then froze after the first tick's ctx was cancelled — the #1812 regression", seen)
		default:
		}
		s, _ := b.Collect(context.Background())
		b.Store(s)
		seen += len(s)
	}

	cancel()
	select {
	case <-src.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("pump goroutine did not exit after app ctx cancel — leaked past shutdown")
	}
}

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
