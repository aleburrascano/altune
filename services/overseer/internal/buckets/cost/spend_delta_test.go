package cost

import (
	"altune/overseer/internal/oci"
	"testing"
	"time"
)

func TestSpendDailyDeltaSamePeriodDecreaseIsZero(t *testing.T) {
	periodStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	baseline := oci.Spend{Amount: 120, PeriodStart: periodStart}
	reading := oci.Spend{Amount: 90, PeriodStart: periodStart}

	got := spendDailyDelta(reading, baseline, true)

	if got != 0 {
		t.Fatalf("spendDailyDelta() = %v, want 0 for a same-period decrease", got)
	}
}

func TestSpendDailyDeltaRollingIntoNewPeriodKeepsFullAmount(t *testing.T) {
	baseline := oci.Spend{Amount: 300, PeriodStart: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)}
	reading := oci.Spend{Amount: 40, PeriodStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}

	got := spendDailyDelta(reading, baseline, true)

	if got != reading.Amount {
		t.Fatalf("spendDailyDelta() = %v, want %v for a period rollover", got, reading.Amount)
	}
}
