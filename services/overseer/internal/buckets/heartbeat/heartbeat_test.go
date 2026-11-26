package heartbeat_test

import (
	"altune/overseer/internal/buckets/heartbeat"
	"altune/overseer/internal/core"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func newHeartbeat() *heartbeat.Bucket {
	return heartbeat.New(heartbeat.Deps{Now: time.Now})
}

func snapshotData(t *testing.T, snap core.Snapshot) heartbeat.Data {
	t.Helper()
	var d heartbeat.Data
	if err := json.Unmarshal(snap.Data, &d); err != nil {
		t.Fatalf("unmarshal data: %v (%s)", err, snap.Data)
	}
	return d
}

func TestHeartbeatCollectStoreSnapshot(t *testing.T) {
	b := newHeartbeat()

	empty := b.Snapshot()
	if empty.State != core.StateLive {
		t.Errorf("empty state = %q, want live", empty.State)
	}
	if got := len(snapshotData(t, empty).Ticks); got != 0 {
		t.Errorf("empty ticks = %d, want 0", got)
	}

	for i := 0; i < 3; i++ {
		signals, err := b.Collect(context.Background())
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		b.Store(signals)
	}

	if got := len(snapshotData(t, b.Snapshot()).Ticks); got != 3 {
		t.Errorf("ticks = %d, want 3", got)
	}
	if b.Meta().ID != "heartbeat" {
		t.Errorf("Meta.ID = %q, want heartbeat", b.Meta().ID)
	}
}

func TestHeartbeatHeadlineTracksTheTicks(t *testing.T) {
	b := newHeartbeat()

	if snap := b.Snapshot(); snap.Severity != core.SeverityOK || snap.Headline != "no ticks yet" {
		t.Errorf("empty grade = %q/%q, want ok/no ticks yet", snap.Severity, snap.Headline)
	}

	for i := 0; i < 3; i++ {
		signals, err := b.Collect(context.Background())
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		b.Store(signals)
	}

	snap := b.Snapshot()
	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok", snap.Severity)
	}
	if snap.Headline != "3 ticks" {
		t.Errorf("headline = %q, want 3 ticks", snap.Headline)
	}
}

func TestHeartbeatStaysBounded(t *testing.T) {
	b := newHeartbeat()
	for i := 0; i < 500; i++ {
		signals, _ := b.Collect(context.Background())
		b.Store(signals)
	}
	if got := len(snapshotData(t, b.Snapshot()).Ticks); got != 60 {
		t.Errorf("ticks = %d, want cap 60", got)
	}
}

func TestHeartbeatWithNoClockUsesTheWallClock(t *testing.T) {
	before := time.Now()
	signals, err := heartbeat.New(heartbeat.Deps{}).Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if len(signals) != 1 || signals[0].At.Before(before) || signals[0].At.After(time.Now()) {
		t.Fatalf("signals = %+v, want one tick stamped by the wall clock", signals)
	}
}

func TestRestoredTicksShowInTheSnapshotAndDateIt(t *testing.T) {
	b := newHeartbeat()
	last := time.Date(2026, 9, 1, 12, 0, 30, 0, time.UTC)
	restored := []core.Signal{{At: last.Add(-30 * time.Second), Kind: "tick", Text: "alive"}, {At: last, Kind: "tick", Text: "alive"}}

	ring := b.Rings()["ticks"]
	if ring == nil {
		t.Fatal("Rings() has no ticks ring")
	}

	ring.Restore(restored)

	snap := b.Snapshot()
	if got := snapshotData(t, snap).Ticks; len(got) != 2 || !got[1].At.Equal(last) {
		t.Fatalf("snapshot ticks = %+v, want the restored %+v", got, restored)
	}
	if !snap.UpdatedAt.Equal(last) {
		t.Fatalf("UpdatedAt = %v, want the newest restored tick %v", snap.UpdatedAt, last)
	}
}

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
		if p.bucket == "heartbeat" && p.series == series {
			out = append(out, p.point)
		}
	}
	return out
}

func fixedClock(ticks []time.Time) func() time.Time {
	i := 0
	return func() time.Time {
		at := ticks[i]
		if i < len(ticks)-1 {
			i++
		}
		return at
	}
}

func TestCollectRecordsTickGapOncePerTickAfterTheFirst(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := heartbeat.New(heartbeat.Deps{Now: fixedClock([]time.Time{base, base.Add(5 * time.Second), base.Add(9 * time.Second)})})
	series := &recordingSeries{}
	b.UseSeries(series)

	for i := 0; i < 3; i++ {
		signals, err := b.Collect(context.Background())
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		b.Store(signals)
	}

	got := series.named("tick_gap_ms")
	if len(got) != 2 {
		t.Fatalf("tick_gap_ms recorded %d points, want 2 (skip the first tick): %+v", len(got), got)
	}
	if got[0].Value != 5000 {
		t.Errorf("first gap = %v, want 5000ms", got[0].Value)
	}
	if got[1].Value != 4000 {
		t.Errorf("second gap = %v, want 4000ms", got[1].Value)
	}
}

func TestKeySeriesIsTickGapMS(t *testing.T) {
	b := newHeartbeat()
	if got := b.KeySeries(); got != "tick_gap_ms" {
		t.Errorf("KeySeries() = %q, want tick_gap_ms", got)
	}
}

func TestHeartbeatBucketIsASeriesWriter(t *testing.T) {
	var bucket core.Bucket = newHeartbeat()
	if _, ok := bucket.(core.SeriesWriter); !ok {
		t.Fatal("heartbeat does not implement core.SeriesWriter, so the composition root never hands it the history store")
	}
}

func TestCollectWithoutSeriesStillCollects(t *testing.T) {
	b := newHeartbeat()
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect with no series wired: %v", err)
	}
}
