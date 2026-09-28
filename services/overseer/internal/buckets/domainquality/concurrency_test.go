package domainquality

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"testing"
	"time"
)

type blockingSeries struct {
	target  string
	entered chan struct{}
	release chan struct{}
}

func newBlockingSeries(target string) *blockingSeries {
	return &blockingSeries{
		target:  target,
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
}

func (s *blockingSeries) Record(_, series string, _ core.Point) {
	if series != s.target {
		return
	}
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-s.release
}

func (s *blockingSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

func assertSnapshotUnblocked(t *testing.T, b *Bucket, series *blockingSeries, collect func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- collect() }()

	select {
	case <-series.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("series.Record was never entered")
	}

	snapshotDone := make(chan struct{})
	go func() {
		b.Snapshot()
		close(snapshotDone)
	}()

	select {
	case <-snapshotDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Snapshot blocked behind an in-flight series.Record call")
	}

	close(series.release)
	if err := <-done; err != nil {
		t.Fatalf("collect: %v", err)
	}
}

func TestSnapshotDoesNotBlockOnInFlightEvalSeriesRecord(t *testing.T) {
	b := newBucket(fakeReader{eval: scoredEval(), acq: healthyAcq()})
	series := newBlockingSeries(seriesEvalScore)
	b.UseSeries(series)

	assertSnapshotUnblocked(t, b, series, func() error {
		_, err := b.Collect(context.Background())
		return err
	})
}

func TestSnapshotDoesNotBlockOnInFlightAcquisitionSeriesRecord(t *testing.T) {
	reader := &steppingAcqReader{
		fakeReader: fakeReader{eval: scoredEval(), disco: goapi.DiscographyQuality{SuspectRate: 0.01}},
		acqs: []goapi.AcquisitionStatus{
			{Succeeded: 90, Failed: 10},
			{Succeeded: 180, Failed: 20},
		},
	}
	b := newBucket(reader)
	series := newBlockingSeries(seriesAcquisitionRate)
	b.UseSeries(series)

	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("seed collect: %v", err)
	}

	assertSnapshotUnblocked(t, b, series, func() error {
		_, err := b.Collect(context.Background())
		return err
	})
}
