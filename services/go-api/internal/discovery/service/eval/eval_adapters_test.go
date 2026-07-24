package eval

import (
	"testing"
)

// metricNames extracts the names for order-insensitive membership checks.
func metricByName(t *testing.T, metrics []NamedMetric) map[string]NamedMetric {
	t.Helper()
	out := make(map[string]NamedMetric, len(metrics))
	for _, m := range metrics {
		out[m.Name] = m
	}
	return out
}

func TestEvalReport_MetricsCorpusPrefix(t *testing.T) {
	plain := EvalReport{Evaluated: 4, Top1Passed: 1, TopKPassed: 2}
	m := metricByName(t, plain.Metrics())
	if got := m["eval.top1_rate"]; got.Value != 0.25 || !got.HigherIsBetter {
		t.Errorf("eval.top1_rate = %+v, want 0.25 higher-is-better", got)
	}
	if got := m["eval.topk_rate"]; got.Value != 0.5 {
		t.Errorf("eval.topk_rate = %+v, want 0.5", got)
	}

	hard := EvalReport{Corpus: "hard", Evaluated: 2, Top1Passed: 1, TopKPassed: 1}
	hm := metricByName(t, hard.Metrics())
	if _, ok := hm["eval.hard_top1_rate"]; !ok {
		t.Errorf("hard corpus must produce distinct gate names, got %v", hm)
	}
}

func TestEvalReport_ZeroEvaluatedRates(t *testing.T) {
	// All-skipped run: evaluated 0 → rates must be 0, not NaN.
	var r EvalReport
	if r.Top1Rate() != 0 || r.TopKRate() != 0 {
		t.Error("zero-evaluated report must yield 0 rates")
	}
	m := metricByName(t, r.Metrics())
	if m["eval.top1_rate"].Value != 0 {
		t.Errorf("metric value = %v, want 0", m["eval.top1_rate"].Value)
	}
}

func TestEvalReport_Failures(t *testing.T) {
	top := &ResultSummary{Kind: "album", Title: "X", Subtitle: "Y"}
	r := EvalReport{Results: []EvalResult{
		{Query: "good one", Outcome: EvalPass},
		{Query: "skipped", Outcome: EvalSkipped},
		{Query: "ghost", Outcome: EvalFailNoResults},
		{Query: "erred", Outcome: EvalFailNoResults, Error: "boom"},
		{Query: "usurped", Outcome: EvalFailWrongTop, Top: top},
	}}
	fails := r.Failures()
	if len(fails) != 3 {
		t.Fatalf("failures = %d, want 3 (pass + skipped excluded)", len(fails))
	}
	reasons := map[string]string{}
	for _, f := range fails {
		reasons[f.Query] = f.Reason
	}
	if reasons["ghost"] != "no_results" || reasons["erred"] != "error" || reasons["usurped"] != "wrong_top" {
		t.Errorf("reasons = %v", reasons)
	}
	for _, f := range fails {
		if f.Query == "usurped" && f.Attrs["top_kind"] != "album" {
			t.Errorf("wrong-top failure must carry top_kind, got %v", f.Attrs)
		}
	}
}

func TestCoverageReportA_MetricsAndFailures(t *testing.T) {
	r := &CoverageReportA{
		Strong:    []CoverageGap{{QueryNorm: "obscure band", Count: 7}},
		Abandoned: []CoverageGap{{QueryNorm: "gave up", Count: 3}, {QueryNorm: "again", Count: 2}},
	}
	m := metricByName(t, r.Metrics())
	if got := m["signal_a.strong_gaps"]; got.Value != 1 || got.HigherIsBetter {
		t.Errorf("strong_gaps = %+v, want 1 lower-is-better", got)
	}
	if got := m["signal_a.abandoned_gaps"]; got.Value != 2 {
		t.Errorf("abandoned_gaps = %+v, want 2", got)
	}

	fails := r.Failures()
	if len(fails) != 3 {
		t.Fatalf("failures = %d, want 3", len(fails))
	}
	if fails[0].Reason != "strong_gap" || fails[0].Attrs["count"] != 7 {
		t.Errorf("strong gap record = %+v", fails[0])
	}
	if fails[1].Reason != "abandoned" {
		t.Errorf("abandoned record = %+v", fails[1])
	}
}

func TestCoverageReportB_MetricsAndFailures(t *testing.T) {
	empty := &CoverageReportB{}
	if got := empty.Metrics()[0].Value; got != 0 {
		t.Errorf("empty report mean gap = %v, want 0 (no division by zero)", got)
	}

	r := &CoverageReportB{ProviderGaps: []ProviderGap{
		{Provider: "deezer", Missing: 1, Union: 10, GapPct: 0.10, Unique: 2},
		{Provider: "itunes", Missing: 3, Union: 10, GapPct: 0.30, Unique: 0},
	}}
	m := r.Metrics()[0]
	if m.Name != "signal_b.mean_gap_pct" || m.Value != 0.20 || m.HigherIsBetter {
		t.Errorf("mean gap metric = %+v, want 0.20 lower-is-better", m)
	}
	fails := r.Failures()
	if len(fails) != 2 || fails[0].Query != "deezer" || fails[0].Reason != "provider_gap" {
		t.Fatalf("failures = %+v", fails)
	}
	if fails[0].Attrs["gap_pct_x100"] != 10 || fails[0].Attrs["unique"] != 2 {
		t.Errorf("attrs = %v", fails[0].Attrs)
	}
}

func TestMergeReport_MetricsAndFailures(t *testing.T) {
	r := MergeReport{
		ResultsSeen:         100,
		UnderMergeIncidents: 5,
		DistinctSeen:        50,
		OverMerged:          1,
		Evaluated:           10,
		CleanQueries:        8,
		Results: []MergeResult{
			{Query: "clean", UnderMergeIncidents: 0},
			{Query: "dupey", UnderMergeIncidents: 2},
		},
	}
	m := metricByName(t, r.Metrics())
	if got := m["merge.under_merge_rate"]; got.Value != 0.05 || got.HigherIsBetter {
		t.Errorf("under_merge_rate = %+v, want 0.05 lower-is-better", got)
	}
	if got := m["merge.over_merge_rate"]; got.Value != 0.02 {
		t.Errorf("over_merge_rate = %+v, want 0.02", got)
	}
	if got := r.CleanMergeRate(); got != 0.8 {
		t.Errorf("CleanMergeRate = %v, want 0.8", got)
	}

	fails := r.Failures()
	if len(fails) != 1 || fails[0].Query != "dupey" || fails[0].Attrs["incidents"] != 2 {
		t.Fatalf("failures = %+v, want only the under-merged query", fails)
	}
}

func TestMergeReport_ZeroDenominators(t *testing.T) {
	var r MergeReport
	if r.UnderMergeRate() != 0 || r.OverMergeRate() != 0 || r.CleanMergeRate() != 0 {
		t.Error("zero-denominator rates must be 0")
	}
}

func TestCorrectionReport_MetricsAndFailures(t *testing.T) {
	r := CorrectionReport{
		Terms:       10,
		Corrupted:   1,
		TyposTested: 20,
		Recovered:   15,
		Corruptions: []FailureRecord{{Query: "valid", Reason: "corrupted_valid_query"}},
		RecallMisses: []FailureRecord{
			{Query: "tpyo", Reason: "not_recovered"},
		},
	}
	m := metricByName(t, r.Metrics())
	if got := m["correction.recall_rate"]; got.Value != 0.75 || !got.HigherIsBetter {
		t.Errorf("recall_rate = %+v, want 0.75 higher-is-better", got)
	}
	if got := m["correction.precision_rate"]; got.Value != 0.9 {
		t.Errorf("precision_rate = %+v, want 0.9", got)
	}
	fails := r.Failures()
	if len(fails) != 2 || fails[0].Reason != "corrupted_valid_query" {
		t.Fatalf("failures = %+v, want corruption leading", fails)
	}
}

func TestCorrectionReport_ZeroDenominators(t *testing.T) {
	var r CorrectionReport
	if r.RecallRate() != 0 || r.PrecisionRate() != 0 {
		t.Error("zero-denominator rates must be 0")
	}
}

func TestDiversityReport_MetricsAndFailures(t *testing.T) {
	loss := FailureRecord{Query: "humble", Reason: "lost_to_reshape"}
	r := DiversityReport{
		Evaluated:            10,
		LostToReshape:        1,
		ConcentrationWith:    0.2,
		ConcentrationWithout: 0.5,
		Losses:               []FailureRecord{loss},
	}
	m := r.Metrics()[0]
	if m.Name != "diversity.cost_rate" || m.Value != 0.1 || m.HigherIsBetter {
		t.Errorf("cost metric = %+v, want diversity.cost_rate 0.1 lower-is-better", m)
	}
	if got := r.ConcentrationDrop(); got < 0.2999 || got > 0.3001 {
		t.Errorf("ConcentrationDrop = %v, want 0.3", got)
	}
	if fails := r.Failures(); len(fails) != 1 || fails[0].Query != "humble" {
		t.Errorf("failures = %+v", fails)
	}

	hard := DiversityReport{Corpus: "hard"}
	if got := hard.Metrics()[0].Name; got != "diversity.hard_cost_rate" {
		t.Errorf("hard corpus metric name = %q", got)
	}
	if hard.CostRate() != 0 {
		t.Error("zero-evaluated cost rate must be 0")
	}
}
