package core_test

import (
	"altune/overseer/internal/core"
	"math"
	"testing"
	"time"
)

var sloEpoch = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func newTestSLO() *core.SLOWindow {
	return core.NewSLOWindow(core.SLO{Journey: "find music", Name: "availability", Target: 0.99})
}

func TestSLOWindowReportsNothingMeasuredWithoutTraffic(t *testing.T) {
	w := newTestSLO()
	w.Record(sloEpoch, 0, 0)

	got := w.Status(sloEpoch)

	if got.Measured || got.Met {
		t.Fatalf("status with no traffic = %+v, want unmeasured and not met", got)
	}
	if got.BudgetRemaining != 1 {
		t.Fatalf("budget with no traffic = %v, want the whole budget", got.BudgetRemaining)
	}
}

func TestSLOWindowComputesAttainmentAndBudget(t *testing.T) {
	w := newTestSLO()
	w.Record(sloEpoch, 990, 1000)
	w.Record(sloEpoch.Add(time.Hour), 495, 500)

	got := w.Status(sloEpoch.Add(2 * time.Hour))

	if !got.Measured || got.Good != 1485 || got.Total != 1500 {
		t.Fatalf("tally = %+v, want 1485 good of 1500", got)
	}
	if math.Abs(got.Attainment-0.99) > 1e-9 || !got.Met {
		t.Fatalf("attainment = %v met = %v, want 0.99 and met", got.Attainment, got.Met)
	}
	if math.Abs(got.BudgetRemaining) > 1e-9 {
		t.Fatalf("budget = %v, want it just spent", got.BudgetRemaining)
	}
}

func TestSLOWindowGoesNegativeWhenTheBudgetIsOverspent(t *testing.T) {
	w := newTestSLO()
	w.Record(sloEpoch, 900, 1000)

	got := w.Status(sloEpoch)

	if got.Met || got.BudgetRemaining >= 0 {
		t.Fatalf("status = %+v, want unmet with a negative budget", got)
	}
}

func TestSLOWindowForgetsDaysOlderThanTheWindow(t *testing.T) {
	w := newTestSLO()
	w.Record(sloEpoch, 0, 100)

	inside := w.Status(sloEpoch.Add((core.SLOWindowDays - 1) * 24 * time.Hour))
	outside := w.Status(sloEpoch.Add(core.SLOWindowDays * 24 * time.Hour))

	if inside.Total != 100 {
		t.Fatalf("last day of the window total = %d, want 100", inside.Total)
	}
	if outside.Total != 0 || outside.Measured {
		t.Fatalf("a day past the window = %+v, want it forgotten", outside)
	}
}

func TestSLOWindowReusesADaySlotWithoutKeepingTheOldDay(t *testing.T) {
	w := newTestSLO()
	w.Record(sloEpoch, 0, 100)
	later := sloEpoch.Add(core.SLOWindowDays * 24 * time.Hour)
	w.Record(later, 50, 50)

	got := w.Status(later)

	if got.Good != 50 || got.Total != 50 {
		t.Fatalf("tally = %+v, want only the recent day", got)
	}
}

func TestSLOWindowClampsGoodToTotal(t *testing.T) {
	w := newTestSLO()
	w.Record(sloEpoch, 20, 10)

	if got := w.Status(sloEpoch); got.Good != 10 || got.Attainment != 1 {
		t.Fatalf("status = %+v, want good clamped to the total", got)
	}
}

func TestNewSLOWindowRefusesATargetOutsideZeroToOne(t *testing.T) {
	for _, target := range []float64{0, 1, -0.5, 1.5} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("target %v: want a panic at construction", target)
				}
			}()
			core.NewSLOWindow(core.SLO{Name: "bad", Target: target})
		}()
	}
}
