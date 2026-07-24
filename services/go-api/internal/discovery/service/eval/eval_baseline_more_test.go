package eval

import (
	"strings"
	"testing"
)

func TestBaselines_GateAll(t *testing.T) {
	b := Baselines{
		"eval.top1_rate":      {Metric: "eval.top1_rate", Value: 0.80, Margin: 0.05, HigherIsBetter: true},
		"diversity.cost_rate": {Metric: "diversity.cost_rate", Value: 0.05, Margin: 0.01, HigherIsBetter: false},
	}
	gates := b.GateAll([]NamedMetric{
		{Name: "eval.top1_rate", Value: 0.70, HigherIsBetter: true},       // regressed
		{Name: "diversity.cost_rate", Value: 0.05, HigherIsBetter: false}, // ok
		{Name: "brand.new", Value: 0.42, HigherIsBetter: true},            // missing
	})
	if len(gates) != 3 {
		t.Fatalf("gates = %d, want 3", len(gates))
	}
	if !gates[0].Regressed || gates[1].Regressed || !gates[2].Missing {
		t.Errorf("gates = %+v", gates)
	}
	if !AnyRegressed(gates) {
		t.Error("AnyRegressed must trip on the first gate")
	}
}

func TestBuildBaselines(t *testing.T) {
	metrics := []NamedMetric{
		{Name: "eval.top1_rate", Value: 0.83, HigherIsBetter: true},
		{Name: "diversity.cost_rate", Value: 0.04, HigherIsBetter: false},
	}
	b := BuildBaselines(metrics, map[string]float64{"eval.top1_rate": 0.02})

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

	// Round-trip: a freshly built baseline gates its own value as not regressed.
	if g := b.Gate("eval.top1_rate", 0.83); g.Regressed {
		t.Error("a value at its own baseline must never regress")
	}
}

func TestGateResult_String(t *testing.T) {
	missing := GateResult{Metric: "new.metric", Current: 0.42, Missing: true}
	if s := missing.String(); !strings.Contains(s, "no baseline") {
		t.Errorf("missing render = %q, want a no-baseline note", s)
	}
	ok := GateResult{Metric: "m", Current: 0.9, Baseline: 0.9, Margin: 0.01}
	if s := ok.String(); !strings.Contains(s, "ok") || strings.Contains(s, "REGRESSED") {
		t.Errorf("ok render = %q", s)
	}
	bad := GateResult{Metric: "m", Current: 0.5, Baseline: 0.9, Margin: 0.01, Regressed: true}
	if s := bad.String(); !strings.Contains(s, "REGRESSED") {
		t.Errorf("regressed render = %q", s)
	}
}

func TestSortedGates(t *testing.T) {
	in := []GateResult{{Metric: "z"}, {Metric: "a"}, {Metric: "m"}}
	got := SortedGates(in)
	if got[0].Metric != "a" || got[1].Metric != "m" || got[2].Metric != "z" {
		t.Errorf("sorted = %v", []string{got[0].Metric, got[1].Metric, got[2].Metric})
	}
	// The input must be untouched (defensive copy).
	if in[0].Metric != "z" {
		t.Error("SortedGates must not mutate its input")
	}
}
