package usage

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"encoding/json"
	"fmt"
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

// find returns the count for label in a rollup list, or -1 if absent.
func find(list []Count, label string) int {
	for _, c := range list {
		if c.Label == label {
			return c.Count
		}
	}
	return -1
}

// TestCollectStoreSnapshotRollups is the core Done proof: search and play events
// flow from the source into bounded rollups and appear in the snapshot as top
// searches, per-kind play counts, and an activity timeline — query text carried
// raw for the frontend to escape.
func TestCollectStoreSnapshotRollups(t *testing.T) {
	src := newFakeSource(16)
	b := newBucket(src)

	if got := len(snapData(t, b.Snapshot()).Searches); got != 0 {
		t.Errorf("empty searches = %d, want 0", got)
	}

	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	src.push(goapi.Event{Type: "search_performed", Timestamp: when, Subject: "jazz"})
	src.push(goapi.Event{Type: "search_performed", Timestamp: when, Subject: "jazz"})
	src.push(goapi.Event{Type: "search_performed", Timestamp: when, Subject: "<script>funk"})
	src.push(goapi.Event{Type: "play", Timestamp: when, Subject: "song a"})
	src.push(goapi.Event{Type: "play", Timestamp: when, Subject: "song b"})
	src.push(goapi.Event{Type: "skip", Timestamp: when, Subject: "song c"})
	collectStore(t, b)

	snap := b.Snapshot()
	if snap.State != core.StateLive {
		t.Errorf("state = %q, want live", snap.State)
	}
	d := snapData(t, snap)
	if got := find(d.Searches, "jazz"); got != 2 {
		t.Errorf("jazz count = %d, want 2", got)
	}
	if got := find(d.Plays, "play"); got != 2 {
		t.Errorf("play count = %d, want 2", got)
	}
	if got := find(d.Plays, "skip"); got != 1 {
		t.Errorf("skip count = %d, want 1", got)
	}
	// The raw, unescaped query is carried verbatim in the payload.
	if got := find(d.Searches, "<script>funk"); got != 1 {
		t.Errorf("raw query count = %d, want 1 (carried unescaped)", got)
	}
	if got := find(d.Timeline, "03:04"); got != 6 {
		t.Errorf("timeline 03:04 = %d, want 6", got)
	}
	if b.Meta().ID != "usage" {
		t.Errorf("Meta.ID = %q, want usage", b.Meta().ID)
	}
}

// TestHeadlineSurvivesTheSourceGoingDown proves the health half is read off the
// payload, not off freshness: the source drops, State flips to source_down, and
// the headline still reports the rollups the bucket is serving. Usage grades ok
// by construction — no amount of searching or playing is a fault.
func TestHeadlineSurvivesTheSourceGoingDown(t *testing.T) {
	src := newFakeSource(8)
	b := newBucket(src)
	src.push(goapi.Event{Type: "search_performed", Subject: "jazz"})
	src.push(goapi.Event{Type: "play", Subject: "song a"})
	collectStore(t, b)
	if got := b.Snapshot().Headline; got != "1 searches · 1 plays" {
		t.Fatalf("live headline = %q, want the rollup totals", got)
	}

	src.setStatus(goapi.StatusDown)
	snap := b.Snapshot()

	if snap.State != core.StateSourceDown {
		t.Fatalf("state = %q, want source_down", snap.State)
	}
	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok — a dropped stream is freshness, not health", snap.Severity)
	}
	if snap.Headline != "1 searches · 1 plays" {
		t.Errorf("headline = %q, want the last-known rollup totals", snap.Headline)
	}
}

// TestDegradesToSourceDownWhenSourceDown is the spine proof: drop the source and
// the snapshot flips to source_down while still carrying the last-known rollups.
func TestDegradesToSourceDownWhenSourceDown(t *testing.T) {
	src := newFakeSource(8)
	b := newBucket(src)
	src.push(goapi.Event{Type: "search_performed", Subject: "last known query"})
	collectStore(t, b)

	if snap := b.Snapshot(); snap.State != core.StateLive {
		t.Fatalf("pre-drop state = %q, want live", snap.State)
	}

	src.setStatus(goapi.StatusDown)

	snap := b.Snapshot()
	if snap.State != core.StateSourceDown {
		t.Errorf("post-drop state = %q, want source_down", snap.State)
	}
	if got := find(snapData(t, snap).Searches, "last known query"); got != 1 {
		t.Errorf("post-drop lost last-known rollups: count=%d", got)
	}
}

// TestDroppedKeysSurfacesCardinalityEviction is the truncation-visibility proof
// for the usage rollup: a flood of distinct one-off queries past the key cap
// reports how many distinct keys were evicted, so the truncation is visible.
func TestDroppedKeysSurfacesCardinalityEviction(t *testing.T) {
	const overflow = 20
	src := newFakeSource(8 * searchKeyCap)
	b := newBucket(src)
	for i := 0; i < searchKeyCap+overflow; i++ {
		src.push(goapi.Event{Type: "search_performed", Subject: fmt.Sprintf("q-%d", i)})
	}
	drainAll(t, b)

	if got := snapData(t, b.Snapshot()).DroppedKeys; got != overflow {
		t.Fatalf("dropped keys = %d, want %d (distinct queries beyond cap)", got, overflow)
	}
}

// TestCollectReportsSourceDownWithNoFreshEvents proves Collect's contract.
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

// TestConcurrentCollectAndSnapshot is the concurrency attack — run under -race.
func TestConcurrentCollectAndSnapshot(t *testing.T) {
	src := newFakeSource(512)
	b := newBucket(src)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kinds := []string{"search_performed", "play", "skip", "completed", "misc"}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			select {
			case src.events <- goapi.Event{Type: kinds[i%len(kinds)], Subject: "q", Timestamp: time.Unix(int64(i), 0)}:
			default:
			}
			signals, _ := b.Collect(ctx)
			b.Store(signals)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = b.Snapshot()
		}
	}()
	wg.Wait()
}

// pumpSource models the real SSE consumer: its Run loop keeps pushing events
// until ITS OWN ctx is cancelled, and it closes stopped when Run returns. That
// lets a test prove the pump both survives past the first collect tick and exits
// cleanly on shutdown, driven through the real Start hook.
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
			case p.events <- goapi.Event{Type: "play", Subject: "song"}:
			default:
			}
		}
	}
}

func (p *pumpSource) Events() <-chan goapi.Event { return p.events }
func (p *pumpSource) Status() goapi.Status       { return goapi.StatusUp }

// TestStartKeepsPumpFeedingPastFirstTick drives the SSE pump through the real
// app-lifetime Start hook (#1950) and proves it keeps feeding the rollups after
// the first collect tick's ctx is cancelled. Before #1950 the pump was launched
// from Collect with the per-tick collect-timeout ctx (#1812), so it froze the
// instant that ctx was cancelled; here a second wave of events proves it now runs
// on the app ctx. Cancelling that ctx returns the pump goroutine, so nothing leaks.
func TestStartKeepsPumpFeedingPastFirstTick(t *testing.T) {
	src := newPumpSource()
	b := newBucket(src)
	appCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	b.Start(appCtx)

	// First tick on its own short-lived ctx, then cancel it — the #1812 per-bucket
	// collect deadline. Under the regression the pump ran on this ctx and froze here.
	firstCtx, firstCancel := context.WithCancel(context.Background())
	first, _ := b.Collect(firstCtx)
	b.Store(first)
	firstCancel()

	// The pump lives on the app ctx, so later ticks keep draining fresh events.
	seen := len(first)
	deadline := time.After(2 * time.Second)
	for seen < len(first)+256 {
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

// TestUnconfiguredDegradesNotCrashes proves the production constructor with no
// go-api env yields a bucket that reports source_down, opens its own null source,
// and never panics.
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

// TestFutureTimestampDoesNotFreezeTimeline is the epic-close hardening proof: a
// single far-future event must not ratchet the activity window into the future.
func TestFutureTimestampDoesNotFreezeTimeline(t *testing.T) {
	src := newFakeSource(8)
	b := newBucket(src)

	src.push(goapi.Event{Type: "play", Timestamp: time.Now().Add(1000 * time.Hour)})
	collectStore(t, b)

	if cur := b.roll.line.curStart; cur.After(time.Now().Add(2 * time.Minute)) {
		t.Errorf("timeline window ratcheted to %v (far future); future timestamp not clamped to now", cur)
	}

	src.push(goapi.Event{Type: "play"})
	collectStore(t, b)
	total := 0
	for _, w := range b.roll.snapshot().timeline {
		total += w.Count
	}
	if total != 2 {
		t.Errorf("timeline counted %d events, want 2", total)
	}
}
