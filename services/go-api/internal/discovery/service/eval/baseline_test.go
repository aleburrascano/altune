package eval_test

import (
	"altune/go-api/internal/discovery"
	"math"
	"strings"
	"testing"
)

func TestBaselines_Gate_higherIsBetter(t *testing.T) {
	b := discovery.Baselines{
		"recall": {Metric: "recall", Value: 0.90, Margin: 0.03, HigherIsBetter: true},
	}
	tests := []struct {
		name      string
		current   float64
		regressed bool
	}{
		{"at baseline", 0.90, false},
		{"inside margin below", 0.88, false},
		{"exactly at threshold", 0.87, false},
		{"below threshold", 0.86, true},
		{"improved", 0.95, false},
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
	b := discovery.Baselines{
		"diversity_cost": {Metric: "diversity_cost", Value: 0.05, Margin: 0.02, HigherIsBetter: false},
	}
	tests := []struct {
		name      string
		current   float64
		regressed bool
	}{
		{"at baseline", 0.05, false},
		{"inside margin above", 0.06, false},
		{"exactly at threshold", 0.07, false},
		{"above threshold", 0.08, true},
		{"improved", 0.01, false},
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

func TestBaselines_Gate_equalAfterFloatArithmeticPasses(t *testing.T) {
	recorded := discovery.EvalMean([]float64{28.0 / 35, 28.0 / 35, 28.0 / 35})
	if recorded == 28.0/35 {
		t.Fatal("precondition: the mean must drift from 28/35 for this test to mean anything")
	}
	lowerRecorded := 1 - recorded
	b := discovery.BuildBaselines([]discovery.NamedMetric{
		{Name: "eval.top1_rate", Value: recorded, HigherIsBetter: true},
		{Name: "miss_rate", Value: lowerRecorded, HigherIsBetter: false},
	}, nil)

	if g := b.Gate("eval.top1_rate", 28.0/35); g.Regressed {
		t.Errorf("higher-is-better: %v equals baseline %v up to rounding, got REGRESSED", g.Current, g.Baseline)
	}
	if g := b.Gate("miss_rate", 1-28.0/35+1e-15); g.Regressed {
		t.Errorf("lower-is-better: %v equals baseline %v up to rounding, got REGRESSED", g.Current, g.Baseline)
	}
	if g := b.Gate("eval.top1_rate", 27.0/35); !g.Regressed {
		t.Error("one case lost against a zero margin must still regress")
	}
	if g := b.Gate("miss_rate", lowerRecorded+1.0/350); !g.Regressed {
		t.Error("a real rise in a lower-is-better metric must still regress")
	}
}

func TestBaselines_Gate_missingIsNeverRegression(t *testing.T) {
	var b discovery.Baselines
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

func TestMeasureNoise(t *testing.T) {
	if m := discovery.MeasureNoise([]float64{0.9}); m != 0 {
		t.Errorf("single sample must yield zero margin, got %v", m)
	}
	m := discovery.MeasureNoise([]float64{0.92, 0.90, 0.94})
	if math.Abs(m-0.06) > 1e-9 {
		t.Errorf("margin = %v, want 0.06", m)
	}
}

func TestMean(t *testing.T) {
	if discovery.EvalMean(nil) != 0 {
		t.Error("mean of empty is 0")
	}
	if got := discovery.EvalMean([]float64{0.90, 0.92, 0.94}); math.Abs(got-0.92) > 1e-9 {
		t.Errorf("mean = %v, want 0.92", got)
	}
}

func TestBaselines_GateAll(t *testing.T) {
	b := discovery.Baselines{
		"eval.top1_rate":      {Metric: "eval.top1_rate", Value: 0.80, Margin: 0.05, HigherIsBetter: true},
		"diversity.cost_rate": {Metric: "diversity.cost_rate", Value: 0.05, Margin: 0.01, HigherIsBetter: false},
	}
	gates := b.GateAll([]discovery.NamedMetric{
		{Name: "eval.top1_rate", Value: 0.70, HigherIsBetter: true},
		{Name: "diversity.cost_rate", Value: 0.05, HigherIsBetter: false},
		{Name: "brand.new", Value: 0.42, HigherIsBetter: true},
	})
	if len(gates) != 3 {
		t.Fatalf("gates = %d, want 3", len(gates))
	}
	if !gates[0].Regressed || gates[1].Regressed || !gates[2].Missing {
		t.Errorf("gates = %+v", gates)
	}
}

func TestBuildBaselines(t *testing.T) {
	metrics := []discovery.NamedMetric{
		{Name: "eval.top1_rate", Value: 0.83, HigherIsBetter: true},
		{Name: "diversity.cost_rate", Value: 0.04, HigherIsBetter: false},
	}
	b := discovery.BuildBaselines(metrics, map[string]float64{"eval.top1_rate": 0.02})

	top1 := b["eval.top1_rate"]
	if top1.Value != 0.83 || top1.Margin != 0.02 || !top1.HigherIsBetter || top1.Metric != "eval.top1_rate" {
		t.Errorf("top1 baseline = %+v", top1)
	}
	cost := b["diversity.cost_rate"]
	if cost.Margin != 0 {
		t.Errorf("unmeasured margin = %v, want 0", cost.Margin)
	}
	if cost.HigherIsBetter {
		t.Error("direction must be carried from the metric")
	}

	if g := b.Gate("eval.top1_rate", 0.83); g.Regressed {
		t.Error("a value at its own baseline must never regress")
	}
}

func TestGateResult_String(t *testing.T) {
	missing := discovery.GateResult{Metric: "new.metric", Current: 0.42, Missing: true}
	if s := missing.String(); !strings.Contains(s, "no baseline") {
		t.Errorf("missing render = %q, want a no-baseline note", s)
	}
	ok := discovery.GateResult{Metric: "m", Current: 0.9, Baseline: 0.9, Margin: 0.01}
	if s := ok.String(); !strings.Contains(s, "ok") || strings.Contains(s, "REGRESSED") {
		t.Errorf("ok render = %q", s)
	}
	bad := discovery.GateResult{Metric: "m", Current: 0.5, Baseline: 0.9, Margin: 0.01, Regressed: true}
	if s := bad.String(); !strings.Contains(s, "REGRESSED") {
		t.Errorf("regressed render = %q", s)
	}
}

func TestSortedGates(t *testing.T) {
	in := []discovery.GateResult{{Metric: "z"}, {Metric: "a"}, {Metric: "m"}}
	got := discovery.SortedGates(in)
	if got[0].Metric != "a" || got[1].Metric != "m" || got[2].Metric != "z" {
		t.Errorf("sorted = %v", []string{got[0].Metric, got[1].Metric, got[2].Metric})
	}
	if in[0].Metric != "z" {
		t.Error("SortedGates must not mutate its input")
	}
}

func TestAnyFailed_AMetricWithNoBaselineFailsTheRun(t *testing.T) {
	baselines := discovery.Baselines{"eval.top1_rate": {Metric: "eval.top1_rate", Value: 0.9, HigherIsBetter: true}}

	gates := baselines.GateAll([]discovery.NamedMetric{
		{Name: "eval.top1_rate", Value: 0.95, HigherIsBetter: true},
		{Name: "merge.under_merge_rate", Value: 0.01},
	})

	if !discovery.AnyFailed(gates) {
		t.Fatal("a metric that is emitted but has no baseline must fail the run")
	}
	if discovery.AnyFailed(gates[:1]) {
		t.Error("a metric inside its baseline must not fail the run")
	}
}

func TestGateResult_FailedCoversBothWaysToFail(t *testing.T) {
	tests := []struct {
		name string
		gate discovery.GateResult
		want bool
	}{
		{"within baseline", discovery.GateResult{}, false},
		{"regressed", discovery.GateResult{Regressed: true}, true},
		{"no baseline", discovery.GateResult{Missing: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.gate.Failed(); got != tt.want {
				t.Errorf("Failed = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBaselines_Gate_AValueExactlyOneToleranceOutsideTheThresholdStillPasses(t *testing.T) {
	b := discovery.Baselines{
		"recall":         {Metric: "recall", Value: 0.5, HigherIsBetter: true},
		"diversity_cost": {Metric: "diversity_cost", Value: 0.5, HigherIsBetter: false},
	}
	if g := b.Gate("recall", 0.5-1e-9); g.Regressed {
		t.Errorf("higher-is-better: %v sits exactly one tolerance below threshold %v, got REGRESSED", g.Current, g.Threshold)
	}
	if g := b.Gate("diversity_cost", 0.5+1e-9); g.Regressed {
		t.Errorf("lower-is-better: %v sits exactly one tolerance above threshold %v, got REGRESSED", g.Current, g.Threshold)
	}
}
