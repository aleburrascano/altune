package domainquality

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
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

func bucketWithSeries(r reader) (*Bucket, *recordingSeries) {
	b := newBucket(r)
	series := &recordingSeries{}
	b.UseSeries(series)
	return b, series
}

func TestCollectRecordsEvalScoreSeries(t *testing.T) {
	b, series := bucketWithSeries(fakeReader{eval: scoredEval(), acq: healthyAcq()})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	got := series.named(seriesEvalScore)
	if len(got) != 1 {
		t.Fatalf("eval_score recorded %d points, want 1: %+v", len(got), got)
	}
	if got[0].Value != 0.81 {
		t.Errorf("eval_score = %v, want 0.81", got[0].Value)
	}
}

func TestCollectRecordsDiscoSuccessRateSeries(t *testing.T) {
	b, series := bucketWithSeries(fakeReader{
		eval:  scoredEval(),
		acq:   healthyAcq(),
		disco: goapi.DiscographyQuality{SuspectRate: 0.25},
	})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	got := series.named(seriesDiscoSuccessRate)
	if len(got) != 1 {
		t.Fatalf("disco_success_rate recorded %d points, want 1: %+v", len(got), got)
	}
	if got[0].Value != 0.75 {
		t.Errorf("disco_success_rate = %v, want 0.75", got[0].Value)
	}
}

func TestCollectRecordsAcquisitionRateOnceWindowed(t *testing.T) {
	reader := &steppingAcqReader{
		fakeReader: fakeReader{eval: scoredEval(), disco: goapi.DiscographyQuality{SuspectRate: 0.01}},
		acqs: []goapi.AcquisitionStatus{
			{Succeeded: 90, Failed: 10},
			{Succeeded: 180, Failed: 20},
		},
	}
	b, series := bucketWithSeries(reader)
	collectN(t, b, 2)

	got := series.named(seriesAcquisitionRate)
	if len(got) != 1 {
		t.Fatalf("acquisition_rate recorded %d points, want 1: %+v", len(got), got)
	}
	if got[0].Value != 0.9 {
		t.Errorf("acquisition_rate = %v, want 0.9", got[0].Value)
	}
}

func TestCollectRecordsNoAcquisitionRateOnFirstCollect(t *testing.T) {
	b, series := bucketWithSeries(fakeReader{eval: scoredEval(), acq: healthyAcq(), disco: goapi.DiscographyQuality{SuspectRate: 0.01}})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if got := series.named(seriesAcquisitionRate); len(got) != 0 {
		t.Fatalf("acquisition_rate recorded %d points on the first collect, want 0 (no window yet): %+v", len(got), got)
	}
}

func TestKeySeriesIsDiscoSuccessRate(t *testing.T) {
	b := newBucket(fakeReader{})
	if got := b.KeySeries(); got != seriesDiscoSuccessRate {
		t.Errorf("KeySeries() = %q, want %q", got, seriesDiscoSuccessRate)
	}
}

func TestBucketIsASeriesWriter(t *testing.T) {
	var bucket core.Bucket = newBucket(fakeReader{})
	if _, ok := bucket.(core.SeriesWriter); !ok {
		t.Fatal("domainquality does not implement core.SeriesWriter, so the composition root never hands it the history store")
	}
}

func TestCollectWithoutSeriesStillCollects(t *testing.T) {
	b := newBucket(fakeReader{eval: scoredEval(), acq: healthyAcq(), disco: goapi.DiscographyQuality{SuspectRate: 0.01}})
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect with no series wired: %v", err)
	}
}
