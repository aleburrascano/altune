package eval

// Eval substrate — baselines and the regression gate shared by every
// discoveryeval harness (plan 2026-06-24-001).
//
// The adequacy bar a harness must meet:
//   - a baseline = the *measured* current value, committed to baselines.json;
//   - a gate = a relative drop below that baseline, never an absolute floor (a
//     hand-picked floor is the query-fit magic constant search.go's doctrine
//     bans);
//   - a margin set *above* measured live-provider noise, so the gate does not
//     flap. The margin is empirical — see MeasureNoise.
//
// Higher-is-better metrics (Top1Rate, recall, fill-rate) regress by dropping;
// lower-is-better metrics (diversity cost, gap count, latency) regress by
// rising. Direction is per-metric.

import (
	"fmt"
	"math"
	"sort"
)

// NamedMetric is one headline number a harness emits for gating. Name is the
// baselines.json key (convention: "<mode>.<metric>", e.g. "eval.top1_rate").
// HigherIsBetter carries the metric's direction so the *first* run can write a
// sensible initial baseline before one is committed.
type NamedMetric struct {
	Name           string  `json:"name"`
	Value          float64 `json:"value"`
	HigherIsBetter bool    `json:"higher_is_better"`
}

// Baseline is one metric's committed reference point plus its empirical noise
// margin. Stored in baselines.json, one entry per metric name.
type Baseline struct {
	Metric         string  `json:"metric"`
	Value          float64 `json:"value"`            // the measured baseline
	Margin         float64 `json:"margin"`           // empirical noise band (>= observed run-to-run spread)
	HigherIsBetter bool    `json:"higher_is_better"` // true for rates; false for cost/latency/gap counts
	Note           string  `json:"note,omitempty"`   // free-text: when/how it was set
}

// Baselines is the committed set, keyed by metric name. The zero value (nil map)
// is usable for reads: every metric is reported Missing until a baseline lands.
type Baselines map[string]Baseline

// GateResult is the verdict for one metric against its baseline. Missing (no
// committed baseline yet) is never a regression — the first run establishes the
// number; it does not fail on it.
type GateResult struct {
	Metric    string  `json:"metric"`
	Current   float64 `json:"current"`
	Baseline  float64 `json:"baseline"`
	Margin    float64 `json:"margin"`
	Threshold float64 `json:"threshold"` // the value current must stay on the safe side of
	Regressed bool    `json:"regressed"`
	Missing   bool    `json:"missing"` // no committed baseline — informational only
}

// Gate compares a freshly measured value against the committed baseline. For a
// higher-is-better metric the threshold is value-margin and a current below it
// regresses; for lower-is-better it is value+margin and a current above it
// regresses. A regression strictly inside the margin is invisible by design —
// the documented tax for a real, live oracle.
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

// GateAll gates a harness's full metric set against the committed baselines in
// one call — the cmd's uniform path across every mode.
func (b Baselines) GateAll(metrics []NamedMetric) []GateResult {
	out := make([]GateResult, 0, len(metrics))
	for _, m := range metrics {
		out = append(out, b.Gate(m.Name, m.Value))
	}
	return out
}

// BuildBaselines turns a freshly measured metric set into committable baselines:
// Value is the measured number, direction is carried from the metric, and Margin
// comes from the optional per-metric noise map (zero when unmeasured). This is
// the explicit re-baseline path — the cmd writes the result to baselines.json
// only on an operator's --update-baselines, never silently.
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

// AnyRegressed reports whether any gate in the set tripped — the signal the cmd
// turns into a non-zero exit code on a nightly / on-demand run.
func AnyRegressed(gates []GateResult) bool {
	for _, g := range gates {
		if g.Regressed {
			return true
		}
	}
	return false
}

// String renders a gate as a one-line status for the report.
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

// MeasureNoise is the empirical noise ritual: given the same metric's value from
// N back-to-back runs, it returns a margin that comfortably covers the observed
// run-to-run spread (peak-to-peak plus headroom). Run a harness 3× once, feed
// the three values here, and commit the result as the baseline's Margin. A
// single sample yields a zero margin (nothing observed yet).
func MeasureNoise(samples []float64) float64 {
	if len(samples) < 2 {
		return 0
	}
	lo, hi := samples[0], samples[0]
	for _, s := range samples[1:] {
		lo = math.Min(lo, s)
		hi = math.Max(hi, s)
	}
	// Peak-to-peak is what we actually saw; 1.5× gives headroom so a slightly
	// wider future swing does not immediately false-red. Empirical, not tuned to
	// any single metric.
	return (hi - lo) * 1.5
}

// Mean is a small helper for recording a baseline from the same N samples used
// to measure noise — the committed Value is their average, not a single run.
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

// SortedGates returns the gates ordered metric-name-ascending for stable report
// output (map iteration order is otherwise random).
func SortedGates(gates []GateResult) []GateResult {
	out := append([]GateResult{}, gates...)
	sort.Slice(out, func(i, j int) bool { return out[i].Metric < out[j].Metric })
	return out
}
