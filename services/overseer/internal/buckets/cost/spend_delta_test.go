package cost_test

import (
	"altune/overseer/internal/oci"
	"testing"
	"time"
)

func dailyAfter(t *testing.T, first, second oci.Spend) float64 {
	t.Helper()
	reader := &fakeReader{}
	reader.set(first, nil)
	b, series := bucketWithSeries(reader, &fakeUsageReader{})
	refreshSpend(t, b, reader)
	reader.set(second, nil)
	refreshSpend(t, b, reader)

	daily := series.named("spend_daily")
	if len(daily) != 2 {
		t.Fatalf("spend_daily recorded %d points across two readings, want 2: %+v", len(daily), daily)
	}
	return daily[1].Value
}

func TestSpendDailyDeltaSamePeriodDecreaseIsZero(t *testing.T) {
	periodStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	got := dailyAfter(t, oci.Spend{Amount: 120, PeriodStart: periodStart}, oci.Spend{Amount: 90, PeriodStart: periodStart})

	if got != 0 {
		t.Fatalf("spend_daily = %v, want 0 for a same-period decrease", got)
	}
}

func TestSpendDailyDeltaRollingIntoNewPeriodKeepsFullAmount(t *testing.T) {
	baseline := oci.Spend{Amount: 300, PeriodStart: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)}
	reading := oci.Spend{Amount: 40, PeriodStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}

	got := dailyAfter(t, baseline, reading)

	if got != reading.Amount {
		t.Fatalf("spend_daily = %v, want %v for a period rollover", got, reading.Amount)
	}
}

func TestTheFirstReadingCountsInFull(t *testing.T) {
	reader := &fakeReader{}
	reader.set(oci.Spend{Amount: 75, PeriodStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}, nil)
	b, series := bucketWithSeries(reader, &fakeUsageReader{})
	refreshSpend(t, b, reader)

	if daily := series.named("spend_daily"); len(daily) != 1 || daily[0].Value != 75 {
		t.Fatalf("spend_daily after the first reading = %+v, want one point of 75", daily)
	}
}
