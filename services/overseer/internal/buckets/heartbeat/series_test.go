package heartbeat

import (
	"altune/overseer/internal/core"
	"context"
	"sync"
	"testing"
	"time"
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
		if p.bucket == bucketID && p.series == series {
			out = append(out, p.point)
		}
	}
	return out
}

// fixedClock returns each of ticks in order on successive calls, for a
// deterministic tick_gap_ms.
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

// TestCollectRecordsTickGapOncePerTickAfterTheFirst proves tick_gap_ms records
// exactly once for every tick after the first — the first tick has no
// predecessor to gap against.
func TestCollectRecordsTickGapOncePerTickAfterTheFirst(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := New()
	b.now = fixedClock([]time.Time{base, base.Add(5 * time.Second), base.Add(9 * time.Second)})
	series := &recordingSeries{}
	b.UseSeries(series)

	for i := 0; i < 3; i++ {
		signals, err := b.Collect(context.Background())
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		b.Store(signals)
	}

	got := series.named(seriesTickGap)
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
	b := New()
	if got := b.KeySeries(); got != seriesTickGap {
		t.Errorf("KeySeries() = %q, want %q", got, seriesTickGap)
	}
}

func TestHeartbeatBucketIsASeriesWriter(t *testing.T) {
	var bucket core.Bucket = New()
	if _, ok := bucket.(core.SeriesWriter); !ok {
		t.Fatal("heartbeat does not implement core.SeriesWriter, so the composition root never hands it the history store")
	}
}

func TestCollectWithoutSeriesStillCollects(t *testing.T) {
	b := New()
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect with no series wired: %v", err)
	}
}
