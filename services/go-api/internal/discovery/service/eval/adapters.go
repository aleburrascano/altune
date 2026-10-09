package eval

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

func (r *CoverageReportA) Metrics() []NamedMetric {
	return []NamedMetric{
		{Name: "signal_a.strong_gaps", Value: float64(len(r.Strong)), HigherIsBetter: false},
		{Name: "signal_a.abandoned_gaps", Value: float64(len(r.Abandoned)), HigherIsBetter: false},
	}
}

func (r *CoverageReportA) Failures() []FailureRecord {
	out := []FailureRecord{}
	for _, g := range r.Strong {
		attrs := QueryAttrs(g.QueryNorm)
		attrs["count"] = g.Count
		out = append(out, FailureRecord{Query: g.QueryNorm, Reason: "strong_gap", Attrs: attrs})
	}
	for _, g := range r.Abandoned {
		attrs := QueryAttrs(g.QueryNorm)
		attrs["count"] = g.Count
		out = append(out, FailureRecord{Query: g.QueryNorm, Reason: "abandoned", Attrs: attrs})
	}
	return out
}

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

func (r MergeReport) Metrics() []NamedMetric {
	return []NamedMetric{
		{Name: "merge.under_merge_rate", Value: r.UnderMergeRate(), HigherIsBetter: false},
		{Name: "merge.over_merge_rate", Value: r.OverMergeRate(), HigherIsBetter: false},
	}
}

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

func (r CorrectionReport) Metrics() []NamedMetric {
	return []NamedMetric{
		{Name: "correction.recall_rate", Value: r.RecallRate(), HigherIsBetter: true},
		{Name: "correction.precision_rate", Value: r.PrecisionRate(), HigherIsBetter: true},
	}
}

func (r CorrectionReport) Failures() []FailureRecord {
	out := make([]FailureRecord, 0, len(r.Corruptions)+len(r.RecallMisses))
	out = append(out, r.Corruptions...)
	out = append(out, r.RecallMisses...)
	return out
}

func (r DiversityReport) Metrics() []NamedMetric {
	name := "diversity.cost_rate"
	if r.Corpus != "" {
		name = "diversity." + r.Corpus + "_cost_rate"
	}
	return []NamedMetric{
		{Name: name, Value: r.CostRate(), HigherIsBetter: false},
	}
}

func (r DiversityReport) Failures() []FailureRecord {
	return r.Losses
}
