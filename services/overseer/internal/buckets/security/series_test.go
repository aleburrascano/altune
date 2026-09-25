package security

import (
	"altune/overseer/internal/core"
	"context"
	"errors"
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

// TestRecordWritesEachSeriesOncePerRun proves one probe run records exactly one
// findings_open point and one probe_failures point, counting a served (failing)
// probe as a finding.
func TestRecordWritesEachSeriesOncePerRun(t *testing.T) {
	b := newBucket(staticProber{status: 401}, defaultSuite(), time.Hour)
	series := &recordingSeries{}
	b.UseSeries(series)

	b.record(runSuite(context.Background(), staticProber{status: 200}, defaultSuite(), time.Now))

	findings := series.named(seriesFindingsOpen)
	if len(findings) != 1 {
		t.Fatalf("findings_open recorded %d points, want 1: %+v", len(findings), findings)
	}
	if findings[0].Value != float64(len(defaultSuite())) {
		t.Errorf("findings_open = %v, want %d (every check served, none rejected)", findings[0].Value, len(defaultSuite()))
	}
	failures := series.named(seriesProbeFailures)
	if len(failures) != 1 {
		t.Fatalf("probe_failures recorded %d points, want 1: %+v", len(failures), failures)
	}
	if failures[0].Value != 0 {
		t.Errorf("probe_failures = %v, want 0 (every check reached go-api)", failures[0].Value)
	}
}

// TestRecordCountsUnreachedRunAsProbeFailures proves a fully-down run — the
// degrade-to-stale path — still records one series point per series, counting
// every check as an unreached probe failure rather than a gap in the series.
func TestRecordCountsUnreachedRunAsProbeFailures(t *testing.T) {
	b := newBucket(staticProber{status: 401}, defaultSuite(), time.Hour)
	series := &recordingSeries{}
	b.UseSeries(series)

	down := runSuite(context.Background(), staticProber{downErr: errors.New("dial tcp: refused")}, defaultSuite(), time.Now)
	b.record(down)

	failures := series.named(seriesProbeFailures)
	if len(failures) != 1 || failures[0].Value != float64(len(defaultSuite())) {
		t.Errorf("probe_failures = %+v, want one point of %d", failures, len(defaultSuite()))
	}
	findings := series.named(seriesFindingsOpen)
	if len(findings) != 1 || findings[0].Value != 0 {
		t.Errorf("findings_open = %+v, want one point of 0 (nothing reached to grade)", findings)
	}
}

func TestKeySeriesIsFindingsOpen(t *testing.T) {
	b := newBucket(staticProber{status: 401}, defaultSuite(), time.Hour)
	if got := b.KeySeries(); got != seriesFindingsOpen {
		t.Errorf("KeySeries() = %q, want %q", got, seriesFindingsOpen)
	}
}

func TestSecurityBucketIsASeriesWriter(t *testing.T) {
	var bucket core.Bucket = newBucket(staticProber{status: 401}, defaultSuite(), time.Hour)
	if _, ok := bucket.(core.SeriesWriter); !ok {
		t.Fatal("security does not implement core.SeriesWriter, so the composition root never hands it the history store")
	}
}
