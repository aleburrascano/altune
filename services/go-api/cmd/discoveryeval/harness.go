package main

// Shared harness runner for the gated discoveryeval modes (plan 2026-06-24-001).
//
// Every gated mode (eval, signal-a, signal-b, merge, correction, diversity)
// flows through runHarness, which gives them one identical spine: run once →
// write JSON → print the mode's human render → gate the headline metrics against
// baselines.json → print the gate block + the attributed-failure slices →
// regress with a non-zero exit. The explicit re-baseline path (--update-baselines)
// runs the harness N times (the noise ritual) and writes measured value + margin.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	discoveryEval "altune/go-api/internal/discovery/service/eval"
)

// errRegressed is the sentinel runHarness returns when a gate trips; main maps
// it to exit code 2 (distinct from operational failures, which exit 1).
var errRegressed = errors.New("eval regression")

// runHarness is the uniform spine for a gated mode. `once` executes the harness
// a single time; `human` renders the mode's existing human-readable report.
func runHarness(
	name string,
	once func() (discoveryEval.HarnessReport, error),
	human func(discoveryEval.HarnessReport) string,
	opts options,
) error {
	if opts.updateBaselines {
		return updateBaselines(name, once, opts)
	}

	report, err := once()
	if err != nil {
		return err
	}
	if err := maybeWriteJSON(opts.jsonPath, report); err != nil {
		return err
	}
	fmt.Print(human(report))

	baselines, err := loadBaselines(opts.baselinesPath)
	if err != nil {
		return err
	}
	gates := baselines.GateAll(report.Metrics())
	fmt.Print(renderGateBlock(gates))
	fmt.Print(renderSlices(report.Failures()))

	if discoveryEval.AnyRegressed(gates) {
		return errRegressed
	}
	return nil
}

// updateBaselines runs the harness opts.noiseRuns times, sets each metric's
// baseline to the mean of the runs and its margin to the measured spread, merges
// the result into the existing baselines file (other modes untouched), and
// writes it back. This is the explicit, operator-invoked re-baseline.
func updateBaselines(
	name string,
	once func() (discoveryEval.HarnessReport, error),
	opts options,
) error {
	runs := opts.noiseRuns
	if runs < 1 {
		runs = 1
	}

	samples := map[string][]float64{}
	directions := map[string]bool{}
	order := []string{}
	for i := 0; i < runs; i++ {
		fmt.Fprintf(os.Stderr, "baseline run %d/%d...\n", i+1, runs)
		report, err := once()
		if err != nil {
			return err
		}
		for _, m := range report.Metrics() {
			if _, seen := samples[m.Name]; !seen {
				order = append(order, m.Name)
			}
			samples[m.Name] = append(samples[m.Name], m.Value)
			directions[m.Name] = m.HigherIsBetter
		}
	}

	metrics := make([]discoveryEval.NamedMetric, 0, len(order))
	margins := map[string]float64{}
	for _, mName := range order {
		metrics = append(metrics, discoveryEval.NamedMetric{
			Name:           mName,
			Value:          discoveryEval.Mean(samples[mName]),
			HigherIsBetter: directions[mName],
		})
		margins[mName] = discoveryEval.MeasureNoise(samples[mName])
	}
	fresh := discoveryEval.BuildBaselines(metrics, margins)

	existing, err := loadBaselines(opts.baselinesPath)
	if err != nil {
		return err
	}
	if existing == nil {
		existing = discoveryEval.Baselines{}
	}
	for k, v := range fresh {
		if runs < 2 {
			v.Note = "single-run baseline — margin 0; re-run with -noise-runs 3 to set a real margin"
		} else {
			v.Note = fmt.Sprintf("measured over %d runs", runs)
		}
		existing[k] = v
	}
	if err := writeBaselines(opts.baselinesPath, existing); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "updated %d baseline(s) for %s in %s\n", len(fresh), name, opts.baselinesPath)
	fmt.Print(renderGateBlock(existing.GateAll(metrics)))
	return nil
}

// ---- baselines file IO --------------------------------------------------

// loadBaselines reads baselines.json. A missing file is not an error — it yields
// an empty set so every metric reports Missing (recorded, never regressed) until
// the first --update-baselines lands.
func loadBaselines(path string) (discoveryEval.Baselines, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return discoveryEval.Baselines{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read baselines %s: %w", path, err)
	}
	var b discoveryEval.Baselines
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parse baselines %s: %w", path, err)
	}
	return b, nil
}

func writeBaselines(path string, b discoveryEval.Baselines) error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal baselines: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write baselines %s: %w", path, err)
	}
	return nil
}

// ---- gate + slice rendering ---------------------------------------------

func renderGateBlock(gates []discoveryEval.GateResult) string {
	out := "\n## Gate\n\n"
	for _, g := range discoveryEval.SortedGates(gates) {
		out += "  " + g.String() + "\n"
	}
	if discoveryEval.AnyRegressed(gates) {
		out += "\n  ⚠ REGRESSION — one or more metrics fell past their noise margin.\n"
	}
	return out
}

// renderSlices is the default view on the attributed failure log: total, then
// the four mechanical single-key slices, then the token×pop joint band where
// ambiguity tends to surface. Disposable sugar — the raw log (in the JSON) is
// the system of record.
func renderSlices(failures []discoveryEval.FailureRecord) string {
	out := fmt.Sprintf("\n## Failure slices — %d total\n\n", len(failures))
	if len(failures) == 0 {
		return out + "_none_\n"
	}
	axes := []struct{ label, key string }{
		{"by token count", discoveryEval.TokenCountAttr},
		{"by script", discoveryEval.ScriptAttr},
		{"by pop band", discoveryEval.PopBandAttr},
		{"by has-id", discoveryEval.HasIDAttr},
	}
	for _, a := range axes {
		buckets := discoveryEval.SliceFailures(failures, a.key)
		// Skip an axis no harness populated (every record "(unset)").
		if len(buckets) == 1 {
			if _, only := buckets["(unset)"]; only {
				continue
			}
		}
		out += fmt.Sprintf("- %-16s %v\n", a.label+":", discoveryEval.TopBuckets(buckets, 8))
	}
	joint := discoveryEval.SliceFailuresByPair(failures, discoveryEval.TokenCountAttr, discoveryEval.PopBandAttr)
	if len(joint) > 1 || func() bool { _, only := joint["(unset)|(unset)"]; return !only }() {
		out += fmt.Sprintf("- %-16s %v\n", "token×pop:", discoveryEval.TopBuckets(joint, 8))
	}
	return out
}
