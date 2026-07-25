package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	discoveryEval "altune/go-api/internal/discovery/service/eval"
)

var errRegressed = errors.New("eval regression")

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
