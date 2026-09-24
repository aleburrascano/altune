package domainquality

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"testing"
	"time"
)

// blockingSeries.Record hangs on the targeted series name until release is
// closed, standing in for the SQLite-backed series store's worst case (up to
// 5s per write). Every other series name is recorded without blocking, so the
// concurrent disco-trend write in the same Collect call never confounds a test
// aimed at eval or acquisition. A Snapshot taken while the targeted Record call
// is in flight must return promptly rather than wait behind it, which is only
// true when the record path releases b.mu before calling Record.
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

// assertSnapshotUnblocked waits for the targeted Record call to be in flight,
// then requires Snapshot to return well inside the blocked call's lifetime.
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

	// The first collect only seeds the acquisition sample ring; the windowed
	// rate, and so the targeted Record call, only fires on the second.
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("seed collect: %v", err)
	}

	assertSnapshotUnblocked(t, b, series, func() error {
		_, err := b.Collect(context.Background())
		return err
	})
}
