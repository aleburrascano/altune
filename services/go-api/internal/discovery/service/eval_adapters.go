package service

// Harness → substrate adapters (plan 2026-06-24-001).
//
// Each existing harness report exposes its headline gateable numbers
// (Metrics) and its attributed failure log (Failures) through one uniform
// contract, so the discoveryeval cmd gates and slices every mode identically.
// Kept in this one file so the tested harness sources (library_eval.go,
// coverage_signal_*.go) are not disturbed.

// HarnessReport is what every eval mode produces: numbers to gate and failures
// to slice. The cmd treats all modes through this single shape.
type HarnessReport interface {
	Metrics() []NamedMetric
	Failures() []FailureRecord
}

var (
	_ HarnessReport = EvalReport{}
	_ HarnessReport = (*CoverageReportA)(nil)
	_ HarnessReport = (*CoverageReportB)(nil)
	_ HarnessReport = MergeReport{}
	_ HarnessReport = CorrectionReport{}
	_ HarnessReport = DiversityReport{}
)

// ---- library eval (ranking) --------------------------------------------

// Metrics gates ranking on both bars: strict #1 and the product top-K bar. The
// corpus tag keeps exact and hard baselines as distinct gate entries.
func (r EvalReport) Metrics() []NamedMetric {
	p := "eval."
	if r.Corpus != "" {
		p = "eval." + r.Corpus + "_"
	}
	return []NamedMetric{
		{Name: p + "top1_rate", Value: r.Top1Rate(), HigherIsBetter: true},
		{Name: p + "topk_rate", Value: r.TopKRate(), HigherIsBetter: true},
	}
}

// Failures emits one attributed record per miss. The query's token count and
// script are always present (the single-token / non-Latin bands); a wrong-top
// miss also carries what kind of result usurped #1.
func (r EvalReport) Failures() []FailureRecord {
	out := []FailureRecord{}
	for _, res := range r.Results {
		if res.Outcome == EvalPass || res.Outcome == EvalSkipped {
			continue
		}
		attrs := QueryAttrs(res.Query)
		reason := "fail"
		switch res.Outcome {
		case EvalFailNoResults:
			reason = "no_results"
			if res.Error != "" {
				reason = "error"
			}
		case EvalFailWrongTop:
			reason = "wrong_top"
			if res.Top != nil {
				attrs["top_kind"] = res.Top.Kind
			}
		}
		out = append(out, FailureRecord{Query: res.Query, Reason: reason, Attrs: attrs})
	}
	return out
}

// ---- coverage signal A (demand-side gaps) -------------------------------

// Metrics gates the strong-gap count (lower is better). It is traffic-sensitive
// — a busy window surfaces more distinct gaps — so its committed margin must be
// wide; the note on the baseline records that.
func (r *CoverageReportA) Metrics() []NamedMetric {
	return []NamedMetric{
		{Name: "signal_a.strong_gaps", Value: float64(len(r.Strong)), HigherIsBetter: false},
	}
}

// Failures: each strong gap is already a failed real user query — emit it with
// its demand weight and the standard query attrs.
func (r *CoverageReportA) Failures() []FailureRecord {
	out := []FailureRecord{}
	for _, g := range r.Strong {
		attrs := QueryAttrs(g.QueryNorm)
		attrs["count"] = g.Count
		out = append(out, FailureRecord{Query: g.QueryNorm, Reason: "strong_gap", Attrs: attrs})
	}
	return out
}

// ---- coverage signal B (provider imbalance) -----------------------------

// Metrics gates the mean per-provider gap (lower is better) — the union-relative
// imbalance across providers.
func (r *CoverageReportB) Metrics() []NamedMetric {
	mean := 0.0
	if len(r.ProviderGaps) > 0 {
		sum := 0.0
		for _, g := range r.ProviderGaps {
			sum += g.GapPct
		}
		mean = sum / float64(len(r.ProviderGaps))
	}
	return []NamedMetric{
		{Name: "signal_b.mean_gap_pct", Value: mean, HigherIsBetter: false},
	}
}

// Failures: one record per provider, attributed with its miss/union counts and
// unique reach. Provider-level by nature — signal B does not retain per-entity
// detail.
func (r *CoverageReportB) Failures() []FailureRecord {
	out := []FailureRecord{}
	for _, g := range r.ProviderGaps {
		out = append(out, FailureRecord{
			Query:  g.Provider,
			Reason: "provider_gap",
			Attrs: map[string]any{
				"missing":      g.Missing,
				"union":        g.Union,
				"gap_pct_x100": int(g.GapPct * 100),
				"unique":       g.Unique,
			},
		})
	}
	return out
}

// ---- merge (precision / recall) -----------------------------------------

// Metrics gates both halves, both lower-is-better: under_merge_rate (recall —
// provable duplicates left unmerged) and over_merge_rate (precision).
func (r MergeReport) Metrics() []NamedMetric {
	return []NamedMetric{
		{Name: "merge.under_merge_rate", Value: r.UnderMergeRate(), HigherIsBetter: false},
		{Name: "merge.over_merge_rate", Value: r.OverMergeRate(), HigherIsBetter: false},
	}
}

// Failures emits the queries that left a provable duplicate unmerged, attributed
// with the standard query bands + the incident count. Over-merges live in the
// report's OverMergeExamples (provenance is a signature pair, not a single query).
func (r MergeReport) Failures() []FailureRecord {
	out := []FailureRecord{}
	for _, res := range r.Results {
		if res.UnderMergeIncidents == 0 {
			continue
		}
		attrs := QueryAttrs(res.Query)
		attrs["incidents"] = res.UnderMergeIncidents
		out = append(out, FailureRecord{Query: res.Query, Reason: "under_merge", Attrs: attrs})
	}
	return out
}

// ---- correction (precision / recall) ------------------------------------

// Metrics gates both halves: recall_rate (typos recovered, higher is better)
// and precision_rate (valid queries left untouched, higher is better).
func (r CorrectionReport) Metrics() []NamedMetric {
	return []NamedMetric{
		{Name: "correction.recall_rate", Value: r.RecallRate(), HigherIsBetter: true},
		{Name: "correction.precision_rate", Value: r.PrecisionRate(), HigherIsBetter: true},
	}
}

// Failures unions the recall misses and the corruptions — both already carry
// their attributed bags. A corruption (rewriting a valid query) is the more
// dangerous failure, so it leads.
func (r CorrectionReport) Failures() []FailureRecord {
	out := make([]FailureRecord, 0, len(r.Corruptions)+len(r.RecallMisses))
	out = append(out, r.Corruptions...)
	out = append(out, r.RecallMisses...)
	return out
}

// ---- diversity (reshaping cost) -----------------------------------------

// Metrics gates ONLY the cost — the correct answers reshaping demotes out of the
// top-K (lower is better). The benefit (concentration drop) is deliberately
// absent: a policy's collateral damage is gated; the policy is not.
func (r DiversityReport) Metrics() []NamedMetric {
	name := "diversity.cost_rate"
	if r.Corpus != "" {
		name = "diversity." + r.Corpus + "_cost_rate"
	}
	return []NamedMetric{
		{Name: name, Value: r.CostRate(), HigherIsBetter: false},
	}
}

// Failures are the entities reshaping pushed below the fold.
func (r DiversityReport) Failures() []FailureRecord {
	return r.Losses
}
