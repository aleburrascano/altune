package cost

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/oci"
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

func bucketWithSeries(spend spendReader, usage usageReader, interval time.Duration) (*Bucket, *recordingSeries) {
	b := newBucket(spend, usage, interval)
	series := &recordingSeries{}
	b.UseSeries(series)
	return b, series
}

func TestRefreshSpendRecordsOnePointOnANewReading(t *testing.T) {
	reader := &fakeReader{}
	reader.set(sampleSpend(), nil)
	b, series := bucketWithSeries(reader, &fakeUsageReader{}, time.Hour)

	refreshSpend(t, b)

	monthToDate := series.named(seriesSpendMonthToDate)
	if len(monthToDate) != 1 {
		t.Fatalf("spend_month_to_date recorded %d points, want 1: %+v", len(monthToDate), monthToDate)
	}
	if got := monthToDate[0].Value; got != sampleSpend().Amount {
		t.Errorf("spend_month_to_date = %v, want %v", got, sampleSpend().Amount)
	}
	if got := series.named(seriesSpendDaily); len(got) != 1 {
		t.Fatalf("spend_daily recorded %d points, want 1: %+v", len(got), got)
	}
}

func TestRefreshSpendRecordsNothingOnARepeatedIdenticalPoll(t *testing.T) {
	reader := &fakeReader{}
	reader.set(sampleSpend(), nil)
	b, series := bucketWithSeries(reader, &fakeUsageReader{}, time.Hour)

	refreshSpend(t, b)
	refreshSpend(t, b)
	refreshSpend(t, b)

	if got := series.named(seriesSpendMonthToDate); len(got) != 1 {
		t.Errorf("spend_month_to_date recorded %d points across 3 identical polls, want 1: %+v", len(got), got)
	}
	if got := series.named(seriesSpendDaily); len(got) != 1 {
		t.Errorf("spend_daily recorded %d points across 3 identical polls, want 1: %+v", len(got), got)
	}
}

func TestRefreshSpendRecordsAgainWhenTheReadingChanges(t *testing.T) {
	reader := &fakeReader{}
	reader.set(sampleSpend(), nil)
	b, series := bucketWithSeries(reader, &fakeUsageReader{}, time.Hour)
	refreshSpend(t, b)

	changed := sampleSpend()
	changed.Amount += 10
	reader.set(changed, nil)
	refreshSpend(t, b)

	monthToDate := series.named(seriesSpendMonthToDate)
	if len(monthToDate) != 2 {
		t.Fatalf("spend_month_to_date recorded %d points across a changed reading, want 2: %+v", len(monthToDate), monthToDate)
	}
	if got := monthToDate[1].Value; got != changed.Amount {
		t.Errorf("spend_month_to_date second point = %v, want %v", got, changed.Amount)
	}
	daily := series.named(seriesSpendDaily)
	if len(daily) != 2 {
		t.Fatalf("spend_daily recorded %d points across a changed reading, want 2: %+v", len(daily), daily)
	}
	if got := daily[1].Value; got != 10 {
		t.Errorf("spend_daily second point = %v, want the 10 delta since the previous reading", got)
	}
}

func TestCollectRecordsProviderCallsPerProvider(t *testing.T) {
	usage := &fakeUsageReader{}
	usage.set(sampleUsage(), nil)
	b, series := bucketWithSeries(&fakeReader{}, usage, time.Hour)

	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if got := series.named(providerCallsSeriesStem + "deezer"); len(got) != 1 || got[0].Value != 126 {
		t.Errorf("provider_calls:deezer = %+v, want one point of 126", got)
	}
	if got := series.named(providerCallsSeriesStem + "spotify"); len(got) != 1 || got[0].Value != 31 {
		t.Errorf("provider_calls:spotify = %+v, want one point of 31", got)
	}
}

func TestKeySeriesIsSpendMonthToDate(t *testing.T) {
	b := newBucket(&fakeReader{}, &fakeUsageReader{}, time.Hour)
	if got := b.KeySeries(); got != seriesSpendMonthToDate {
		t.Errorf("KeySeries() = %q, want %q", got, seriesSpendMonthToDate)
	}
}

func TestBucketIsASeriesWriter(t *testing.T) {
	var bucket core.Bucket = newBucket(&fakeReader{}, &fakeUsageReader{}, time.Hour)
	if _, ok := bucket.(core.SeriesWriter); !ok {
		t.Fatal("cost does not implement core.SeriesWriter, so the composition root never hands it the history store")
	}
}

func TestCollectAndRefreshSpendWithoutSeriesStillWork(t *testing.T) {
	spend := &fakeReader{}
	spend.set(sampleSpend(), nil)
	usage := &fakeUsageReader{}
	usage.set(sampleUsage(), nil)
	b := newBucket(spend, usage, time.Hour)

	b.refreshSpend(context.Background())
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect with no series wired: %v", err)
	}
}

func TestSpendDailyResetsAcrossANewBillingPeriod(t *testing.T) {
	reader := &fakeReader{}
	first := sampleSpend()
	reader.set(first, nil)
	b, series := bucketWithSeries(reader, &fakeUsageReader{}, time.Hour)
	refreshSpend(t, b)

	next := oci.Spend{
		Amount:      5,
		Currency:    first.Currency,
		PeriodStart: first.PeriodStart.AddDate(0, 1, 0),
		PeriodEnd:   first.PeriodEnd.AddDate(0, 1, 0),
	}
	reader.set(next, nil)
	refreshSpend(t, b)

	daily := series.named(seriesSpendDaily)
	if len(daily) != 2 {
		t.Fatalf("spend_daily recorded %d points across a period rollover, want 2: %+v", len(daily), daily)
	}
	if daily[1].Value != next.Amount {
		t.Errorf("spend_daily after rollover = %v, want the fresh period's whole amount %v", daily[1].Value, next.Amount)
	}
}
