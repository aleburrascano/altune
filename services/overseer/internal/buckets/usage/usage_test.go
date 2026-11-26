package usage_test

import (
	"altune/overseer/internal/buckets/usage"
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

type eventSource interface {
	Run(ctx context.Context) error
	Events() <-chan goapi.Event
	Status() goapi.Status
}

func newBucket(src eventSource) *usage.Bucket {
	return usage.New(usage.Deps{Source: src})
}

func collectStore(t *testing.T, b *usage.Bucket) {
	t.Helper()
	signals, err := b.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	b.Store(signals)
}

func snapData(t *testing.T, snap core.Snapshot) usage.Data {
	t.Helper()
	var d usage.Data
	if err := json.Unmarshal(snap.Data, &d); err != nil {
		t.Fatalf("unmarshal data: %v (%s)", err, snap.Data)
	}
	return d
}

func find(list []usage.Count, label string) int {
	for _, c := range list {
		if c.Label == label {
			return c.Count
		}
	}
	return -1
}

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

func TestUnconfiguredDegradesNotCrashes(t *testing.T) {
	b := usage.New(usage.Deps{})

	if snap := b.Snapshot(); snap.State != core.StateSourceDown {
		t.Errorf("unconfigured state = %q, want source_down", snap.State)
	}
	if _, err := b.Collect(context.Background()); err == nil {
		t.Error("unconfigured Collect returned nil, want the source-down error")
	}
}

func TestFutureTimestampDoesNotFreezeTimeline(t *testing.T) {
	src := newFakeSource(8)
	b := newBucket(src)
	before := time.Now()

	src.push(goapi.Event{Type: "play", Timestamp: before.Add(1000 * time.Hour)})
	collectStore(t, b)
	src.push(goapi.Event{Type: "play"})
	collectStore(t, b)
	after := time.Now()

	now := map[string]bool{before.Format("15:04"): true, after.Format("15:04"): true}
	total := 0
	for _, w := range snapData(t, b.Snapshot()).Timeline {
		if !now[w.Label] {
			t.Errorf("timeline window %q is not the current minute; a future timestamp was not clamped to now", w.Label)
		}
		total += w.Count
	}
	if total != 2 {
		t.Errorf("timeline counted %d events, want 2", total)
	}
}

func TestReconnectingSourceReportsConnectingReason(t *testing.T) {
	cases := []struct {
		status     goapi.Status
		wantState  core.State
		wantReason string
	}{
		{goapi.StatusConnecting, core.StateStale, "connecting"},
		{goapi.StatusUp, core.StateLive, ""},
	}
	for _, tc := range cases {
		t.Run(tc.status.String(), func(t *testing.T) {
			src := newFakeSource(1)
			src.setStatus(tc.status)

			snap := newBucket(src).Snapshot()

			if snap.State != tc.wantState || snap.Reason != tc.wantReason {
				t.Errorf("snapshot state %q reason %q, want %q/%q", snap.State, snap.Reason, tc.wantState, tc.wantReason)
			}
		})
	}
}

const (
	requestsPerMin = "requests_per_min"
	activeUsers    = "active_users"
)

type recordedPoint struct {
	bucket string
	series string
	point  core.Point
}

type recordingSeries struct {
	mu     sync.Mutex
	points []recordedPoint
}

func (r *recordingSeries) Record(bucket, series string, p core.Point) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.points = append(r.points, recordedPoint{bucket: bucket, series: series, point: p})
}

func (r *recordingSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

func (r *recordingSeries) named(series string) []core.Point {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []core.Point
	for _, p := range r.points {
		if p.bucket == "usage" && p.series == series {
			out = append(out, p.point)
		}
	}
	return out
}

func bucketWithSeries(src eventSource) (*usage.Bucket, *recordingSeries) {
	b := newBucket(src)
	series := &recordingSeries{}
	b.UseSeries(series)
	return b, series
}

func TestCollectRecordsRequestsAndUsersOncePerCompletedWindow(t *testing.T) {
	src := newFakeSource(16)
	b, series := bucketWithSeries(src)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	src.push(goapi.Event{Type: "search_performed", Timestamp: base, User: "alice", Subject: "jazz"})
	src.push(goapi.Event{Type: "play", Timestamp: base.Add(10 * time.Second), User: "bob"})
	src.push(goapi.Event{Type: "play", Timestamp: base.Add(20 * time.Second), User: "alice"})
	collectStore(t, b)

	if got := series.named(requestsPerMin); len(got) != 0 {
		t.Fatalf("requests_per_min recorded %d points before the window completed, want 0: %+v", len(got), got)
	}

	src.push(goapi.Event{Type: "play", Timestamp: base.Add(90 * time.Second), User: "carol"})
	collectStore(t, b)

	reqPoints := series.named(requestsPerMin)
	if len(reqPoints) != 1 {
		t.Fatalf("requests_per_min recorded %d points, want 1: %+v", len(reqPoints), reqPoints)
	}
	if reqPoints[0].Value != 3 {
		t.Errorf("requests_per_min = %v, want 3", reqPoints[0].Value)
	}
	userPoints := series.named(activeUsers)
	if len(userPoints) != 1 {
		t.Fatalf("active_users recorded %d points, want 1: %+v", len(userPoints), userPoints)
	}
	if userPoints[0].Value != 2 {
		t.Errorf("active_users = %v, want 2 (alice, bob)", userPoints[0].Value)
	}
}

func TestKeySeriesIsRequestsPerMin(t *testing.T) {
	b := newBucket(newFakeSource(1))
	if got := b.KeySeries(); got != requestsPerMin {
		t.Errorf("KeySeries() = %q, want %q", got, requestsPerMin)
	}
}

func TestBucketIsASeriesWriter(t *testing.T) {
	var bucket core.Bucket = newBucket(newFakeSource(1))
	if _, ok := bucket.(core.SeriesWriter); !ok {
		t.Fatal("usage does not implement core.SeriesWriter, so the composition root never hands it the history store")
	}
}

func TestCollectWithoutSeriesStillCollects(t *testing.T) {
	src := newFakeSource(1)
	b := newBucket(src)
	src.push(goapi.Event{Type: "play", Subject: "song"})

	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect with no series wired: %v", err)
	}
}

type droppingSource struct {
	*fakeSource
	streamDrops int
}

func (d droppingSource) Dropped() int { return d.streamDrops }

func TestSnapshotDroppedReportsStreamDrops(t *testing.T) {
	const streamDrops = 9
	b := newBucket(droppingSource{fakeSource: newFakeSource(1), streamDrops: streamDrops})

	if got := snapData(t, b.Snapshot()).Dropped; got != streamDrops {
		t.Fatalf("snapshot dropped = %d, want %d stream drops", got, streamDrops)
	}
}
