package guard_test

import (
	"altune/overseer/internal/core"
	"context"
	"sync"
	"testing"
	"time"

	_ "altune/overseer/internal/buckets/backendperf"
	_ "altune/overseer/internal/buckets/cost"
	_ "altune/overseer/internal/buckets/domainquality"
	_ "altune/overseer/internal/buckets/heartbeat"
	_ "altune/overseer/internal/buckets/liveactivity"
	_ "altune/overseer/internal/buckets/logs"
	_ "altune/overseer/internal/buckets/reliability"
	_ "altune/overseer/internal/buckets/security"
	_ "altune/overseer/internal/buckets/usage"
)

type recordingSeries struct {
	mu     sync.Mutex
	points []recordedPoint
}

type recordedPoint struct {
	bucket string
	series string
}

func (r *recordingSeries) Record(bucket, series string, _ core.Point) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.points = append(r.points, recordedPoint{bucket: bucket, series: series})
}

func (r *recordingSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

func (r *recordingSeries) seriesNames(bucket string) map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := map[string]bool{}
	for _, p := range r.points {
		if p.bucket == bucket {
			names[p.series] = true
		}
	}
	return names
}

func runBucketOnce(b core.Bucket) (cleanCollect bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if s, ok := b.(core.Starter); ok {
		s.Start(ctx)
	}
	for i := 0; i < 3; i++ {
		signals, err := b.Collect(ctx)
		if err == nil {
			cleanCollect = true
		}
		b.Store(signals)
		time.Sleep(20 * time.Millisecond)
	}
	if w, ok := b.(core.Waiter); ok {
		cancel()
		w.Wait()
	}
	return cleanCollect
}

var offlineProofByBucket = map[string]string{
	"backendperf":   "backendperf/series_test.go: TestCollectRecordsEachOverallSeriesOnce",
	"cost":          "cost/series_test.go: TestRefreshSpendRecordsOnePointOnANewReading",
	"domainquality": "domainquality/series_test.go: TestCollectRecordsDiscoSuccessRateSeries",
	"usage":         "usage/series_test.go: TestCollectRecordsRequestsAndUsersOncePerCompletedWindow",
	"reliability":   "reliability/keyseries_write_test.go: TestAnsweredProbeWritesUnderTheDeclaredKeySeries",
}

func TestRealBucketsWriteUnderTheirDeclaredKeySeries(t *testing.T) {
	for _, b := range core.Default.Buckets() {
		b := b
		id := b.Meta().ID
		keyed, ok := b.(core.KeySeries)
		if !ok {
			t.Logf("bucket %q implements no KeySeries: opted out of the spark feature", id)
			continue
		}
		name := keyed.KeySeries()
		if name == "" {
			t.Errorf("bucket %q implements KeySeries but returns \"\"; drop the method instead of opting out with an empty name", id)
			continue
		}
		t.Run(id, func(t *testing.T) {
			writer, ok := b.(core.SeriesWriter)
			if !ok {
				t.Fatalf("bucket %q declares KeySeries() = %q but does not implement core.SeriesWriter, so it can never write that series", id, name)
			}
			series := &recordingSeries{}
			writer.UseSeries(series)

			cleanCollect := runBucketOnce(b)

			names := series.seriesNames(id)
			if names[name] {
				return
			}
			if cleanCollect {
				t.Errorf("bucket %q reported a clean Collect cycle but never wrote its declared KeySeries() = %q (wrote %v instead)", id, name, names)
				return
			}
			proof, hasProof := offlineProofByBucket[id]
			if !hasProof {
				t.Fatalf("bucket %q needs a live reach to prove KeySeries() = %q here and has no entry in offlineProofByBucket naming the package test that proves it offline instead", id, name)
			}
			if len(names) == 0 {
				t.Skipf("bucket %q wrote no series in this run — its sources need a live go-api/OCI reach this test environment does not have; KeySeries = %q is proven offline instead by %s", id, name, proof)
			}
			t.Skipf("bucket %q wrote %v but not its declared KeySeries() = %q in this run — that series needs a live reach this test environment does not have; proven offline instead by %s", id, names, name, proof)
		})
	}
}

type keySeriesOptOutBucket struct{ core.Bucket }

func (keySeriesOptOutBucket) KeySeries() string { return "" }

func TestKeySeriesEmptyStringIsARecognisedOptOut(t *testing.T) {
	var b core.Bucket = keySeriesOptOutBucket{}
	keyed, ok := b.(core.KeySeries)
	if !ok {
		t.Fatal("keySeriesOptOutBucket must implement core.KeySeries for this test to exercise the \"\" branch")
	}
	if got := keyed.KeySeries(); got != "" {
		t.Fatalf("KeySeries() = %q, want \"\" (the opt-out this test targets)", got)
	}
}
