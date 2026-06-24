package service

import (
	"math"
	"testing"
)

func TestBaselines_Gate_higherIsBetter(t *testing.T) {
	b := Baselines{
		"recall": {Metric: "recall", Value: 0.90, Margin: 0.03, HigherIsBetter: true},
	}
	tests := []struct {
		name      string
		current   float64
		regressed bool
	}{
		{"at baseline", 0.90, false},
		{"inside margin below", 0.88, false},   // 0.90-0.03=0.87 threshold; 0.88 ok
		{"exactly at threshold", 0.87, false},  // not strictly below
		{"below threshold", 0.86, true},        // regressed
		{"improved", 0.95, false},              // higher is fine
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := b.Gate("recall", tt.current)
			if g.Missing {
				t.Fatal("unexpected missing")
			}
			if g.Regressed != tt.regressed {
				t.Errorf("current %.2f: regressed=%v want %v (threshold %.2f)", tt.current, g.Regressed, tt.regressed, g.Threshold)
			}
		})
	}
}

func TestBaselines_Gate_lowerIsBetter(t *testing.T) {
	b := Baselines{
		"diversity_cost": {Metric: "diversity_cost", Value: 0.05, Margin: 0.02, HigherIsBetter: false},
	}
	tests := []struct {
		name      string
		current   float64
		regressed bool
	}{
		{"at baseline", 0.05, false},
		{"inside margin above", 0.06, false},  // 0.05+0.02=0.07 threshold; 0.06 ok
		{"exactly at threshold", 0.07, false}, // not strictly above
		{"above threshold", 0.08, true},       // cost rose — regressed
		{"improved", 0.01, false},             // lower cost is fine
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := b.Gate("diversity_cost", tt.current)
			if g.Regressed != tt.regressed {
				t.Errorf("current %.2f: regressed=%v want %v (threshold %.2f)", tt.current, g.Regressed, tt.regressed, g.Threshold)
			}
		})
	}
}

func TestBaselines_Gate_missingIsNeverRegression(t *testing.T) {
	var b Baselines // nil map
	g := b.Gate("brand_new_metric", 0.42)
	if !g.Missing {
		t.Fatal("expected missing for an uncommitted metric")
	}
	if g.Regressed {
		t.Error("a missing baseline must never count as a regression")
	}
	if g.Current != 0.42 {
		t.Errorf("current not carried through: %v", g.Current)
	}
}

func TestAnyRegressed(t *testing.T) {
	if AnyRegressed([]GateResult{{Regressed: false}, {Missing: true}}) {
		t.Error("no gate regressed")
	}
	if !AnyRegressed([]GateResult{{Regressed: false}, {Regressed: true}}) {
		t.Error("one gate regressed — should report true")
	}
}

func TestMeasureNoise(t *testing.T) {
	if m := MeasureNoise([]float64{0.9}); m != 0 {
		t.Errorf("single sample must yield zero margin, got %v", m)
	}
	// spread 0.90..0.94 = 0.04 peak-to-peak, ×1.5 = 0.06
	m := MeasureNoise([]float64{0.92, 0.90, 0.94})
	if math.Abs(m-0.06) > 1e-9 {
		t.Errorf("margin = %v, want 0.06", m)
	}
}

func TestMean(t *testing.T) {
	if Mean(nil) != 0 {
		t.Error("mean of empty is 0")
	}
	if got := Mean([]float64{0.90, 0.92, 0.94}); math.Abs(got-0.92) > 1e-9 {
		t.Errorf("mean = %v, want 0.92", got)
	}
}
