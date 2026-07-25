package eval

import (
	"fmt"
	"math"
	"sort"
)

type NamedMetric struct {
	Name           string  `json:"name"`
	Value          float64 `json:"value"`
	HigherIsBetter bool    `json:"higher_is_better"`
}

type Baseline struct {
	Metric         string  `json:"metric"`
	Value          float64 `json:"value"`
	Margin         float64 `json:"margin"`
	HigherIsBetter bool    `json:"higher_is_better"`
	Note           string  `json:"note,omitempty"`
}

type Baselines map[string]Baseline

type GateResult struct {
	Metric    string  `json:"metric"`
	Current   float64 `json:"current"`
	Baseline  float64 `json:"baseline"`
	Margin    float64 `json:"margin"`
	Threshold float64 `json:"threshold"`
	Regressed bool    `json:"regressed"`
	Missing   bool    `json:"missing"`
}

func (b Baselines) Gate(metric string, current float64) GateResult {
	base, ok := b[metric]
	if !ok {
		return GateResult{Metric: metric, Current: current, Missing: true}
	}

	res := GateResult{
		Metric:   metric,
		Current:  current,
		Baseline: base.Value,
		Margin:   base.Margin,
	}
	if base.HigherIsBetter {
		res.Threshold = base.Value - base.Margin
		res.Regressed = current < res.Threshold
	} else {
		res.Threshold = base.Value + base.Margin
		res.Regressed = current > res.Threshold
	}
	return res
}

func (b Baselines) GateAll(metrics []NamedMetric) []GateResult {
	out := make([]GateResult, 0, len(metrics))
	for _, m := range metrics {
		out = append(out, b.Gate(m.Name, m.Value))
	}
	return out
}

func BuildBaselines(metrics []NamedMetric, margins map[string]float64) Baselines {
	out := make(Baselines, len(metrics))
	for _, m := range metrics {
		out[m.Name] = Baseline{
			Metric:         m.Name,
			Value:          m.Value,
			Margin:         margins[m.Name],
			HigherIsBetter: m.HigherIsBetter,
		}
	}
	return out
}

func AnyRegressed(gates []GateResult) bool {
	for _, g := range gates {
		if g.Regressed {
			return true
		}
	}
	return false
}

func (g GateResult) String() string {
	if g.Missing {
		return fmt.Sprintf("%-28s %8.4f  (no baseline — recorded)", g.Metric, g.Current)
	}
	status := "ok"
	if g.Regressed {
		status = "REGRESSED"
	}
	return fmt.Sprintf("%-28s %8.4f  base=%.4f ±%.4f  %s", g.Metric, g.Current, g.Baseline, g.Margin, status)
}

func MeasureNoise(samples []float64) float64 {
	if len(samples) < 2 {
		return 0
	}
	lo, hi := samples[0], samples[0]
	for _, s := range samples[1:] {
		lo = math.Min(lo, s)
		hi = math.Max(hi, s)
	}
	return (hi - lo) * 1.5
}

func Mean(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sum := 0.0
	for _, s := range samples {
		sum += s
	}
	return sum / float64(len(samples))
}

func SortedGates(gates []GateResult) []GateResult {
	out := append([]GateResult{}, gates...)
	sort.Slice(out, func(i, j int) bool { return out[i].Metric < out[j].Metric })
	return out
}
