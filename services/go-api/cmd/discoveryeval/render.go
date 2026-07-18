package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	discoveryEval "altune/go-api/internal/discovery/service/eval"
)

func maybeWriteJSON(path string, report any) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	fmt.Fprintf(os.Stderr, "wrote JSON report to %s\n", path)
	return nil
}

func renderEval(report discoveryEval.EvalReport) string {
	out := fmt.Sprintf("# Discovery library eval — %s\n\n", time.Now().UTC().Format(time.RFC3339))
	out += fmt.Sprintf("- Total: %d (evaluated %d, skipped %d)\n", report.Total, report.Evaluated, report.Skipped)
	out += fmt.Sprintf("- Top-1: %d (%.1f%%)\n", report.Top1Passed, report.Top1Rate()*100)
	out += fmt.Sprintf("- Top-%d: %d (%.1f%%) — of which %d ranked below #1\n", report.K, report.TopKPassed, report.TopKRate()*100, report.TopKPassed-report.Top1Passed)
	out += fmt.Sprintf("- Failed (not in top-%d): %d\n", report.K, report.Failed)
	if len(report.FailuresByTopKind) > 0 {
		out += "- Failures by top result kind:"
		for kind, n := range report.FailuresByTopKind {
			out += fmt.Sprintf(" %s=%d", kind, n)
		}
		out += "\n"
	}

	out += "\n## Failures\n\n"
	failures := 0
	for _, r := range report.Results {
		if r.Outcome == discoveryEval.EvalPass || r.Outcome == discoveryEval.EvalSkipped {
			continue
		}
		failures++
		switch r.Outcome {
		case discoveryEval.EvalFailNoResults:
			reason := "no results"
			if r.Error != "" {
				reason = "error: " + r.Error
			}
			out += fmt.Sprintf("- %q → %s\n", r.Query, reason)
		case discoveryEval.EvalFailWrongTop:
			out += fmt.Sprintf("- %q → #1 was [%s] %q — %s\n", r.Query, r.Top.Kind, r.Top.Title, r.Top.Subtitle)
		}
	}
	if failures == 0 {
		out += "_none_\n"
	}
	return out
}

func renderArtistIntent(report discoveryEval.ArtistIntentReport) string {
	out := fmt.Sprintf("# Discovery artist-intent eval — %s\n\n", time.Now().UTC().Format(time.RFC3339))
	out += fmt.Sprintf("- Total: %d (evaluated %d, skipped %d)\n", report.Total, report.Evaluated, report.Skipped)
	out += fmt.Sprintf("- Top-1: %d (%.1f%%)\n", report.Top1Passed, report.Top1Rate()*100)
	out += fmt.Sprintf("- Top-%d: %d (%.1f%%) — the product bar\n", report.K, report.TopKPassed, report.TopKRate()*100)
	out += fmt.Sprintf("- BURIED (ranker bug): %d (%.1f%%) — artist present but a same-name track out-ranked it\n", report.Buried, report.BuriedRate()*100)
	out += fmt.Sprintf("- ABSENT (recall gap): %d (%.1f%%) — artist card never surfaced; no reorder can fix\n", report.Absent, report.AbsentRate()*100)
	out += fmt.Sprintf("- Below-K (other): %d · No-results: %d\n", report.BelowK, report.NoResults)

	out += "\n## Buried — artist out-ranked by a same-name track (ranker-fixable)\n\n"
	buried, absent := 0, 0
	for _, r := range report.Results {
		if r.Outcome != discoveryEval.ArtistIntentBuried {
			continue
		}
		buried++
		out += fmt.Sprintf("- %q → artist at #%d, first same-name track at #%d (top: [%s] %q)\n",
			r.Artist, r.ArtistPos+1, r.FirstTrackPos+1, r.Top.Kind, r.Top.Title)
	}
	if buried == 0 {
		out += "_none_\n"
	}

	out += "\n## Absent — artist card never surfaced (recall/identity gap)\n\n"
	for _, r := range report.Results {
		if r.Outcome != discoveryEval.ArtistIntentAbsent {
			continue
		}
		absent++
		out += fmt.Sprintf("- %q (top: [%s] %q — %s)\n", r.Artist, r.Top.Kind, r.Top.Title, r.Top.Subtitle)
	}
	if absent == 0 {
		out += "_none_\n"
	}
	return out
}

func renderMerge(report discoveryEval.MergeReport) string {
	out := fmt.Sprintf("# Discovery merge eval — %s\n\n", time.Now().UTC().Format(time.RFC3339))
	out += fmt.Sprintf("- Total: %d (evaluated %d, no-match %d, skipped %d)\n", report.Total, report.Evaluated, report.NoMatch, report.Skipped)
	out += fmt.Sprintf("- Under-merge rate: %.2f%% (%d provable dups unmerged of %d rows; %.1f%% queries clean)\n",
		report.UnderMergeRate()*100, report.UnderMergeIncidents, report.ResultsSeen, report.CleanMergeRate()*100)
	out += fmt.Sprintf("- Over-merge rate: %.2f%% (%d of %d distinct entities)\n", report.OverMergeRate()*100, report.OverMerged, report.DistinctSeen)

	if len(report.OverMergeExamples) > 0 {
		out += "\n## Over-merges (distinct owned titles folded into one entity)\n\n"
		for _, ex := range report.OverMergeExamples {
			out += fmt.Sprintf("- %s\n", ex)
		}
	}

	out += "\n## Under-merges (provable duplicate left as separate rows)\n\n"
	if len(report.UnderMergeExamples) == 0 {
		out += "_none_\n"
	}
	for _, ex := range report.UnderMergeExamples {
		out += fmt.Sprintf("- %s\n", ex)
	}
	return out
}

func renderHealth(report discoveryEval.HealthReport) string {
	out := fmt.Sprintf("# Discovery health gauges (report-only) — %s\n\n", time.Now().UTC().Format(time.RFC3339))
	out += fmt.Sprintf("- Searches: %d, result rows: %d\n", report.Searches, report.Results)
	out += fmt.Sprintf("- Artwork fill-rate: %.1f%% (%d of %d)\n", report.FillRate*100, report.WithArtwork, report.Results)
	out += fmt.Sprintf("- Identity-bridge hit-rate: %.1f%% (%d merged via bridge)\n", report.BridgeHitRate*100, report.BridgedMerges)
	out += fmt.Sprintf("- Latency: p50 %dms · p95 %dms · max %dms\n", report.LatencyP50Ms, report.LatencyP95Ms, report.LatencyMaxMs)
	out += "\n_These gauges are tracked for visibility only — they never gate._\n"
	return out
}

func renderDiversity(report discoveryEval.DiversityReport) string {
	out := fmt.Sprintf("# Discovery diversity differential — %s\n\n", time.Now().UTC().Format(time.RFC3339))
	out += fmt.Sprintf("- Evaluated: %d of %d\n", report.Evaluated, report.Total)
	out += fmt.Sprintf("- COST (gated): %.2f%% lost to reshape (%d demoted out of top-%d), %d gained\n",
		report.CostRate()*100, report.LostToReshape, report.K, report.GainedByReshape)
	out += fmt.Sprintf("- BENEFIT (report-only): top-%d concentration %.3f → %.3f (drop %.3f)\n",
		report.K, report.ConcentrationWithout, report.ConcentrationWith, report.ConcentrationDrop())

	out += "\n## Lost to reshape (correct result demoted below the fold)\n\n"
	if len(report.Losses) == 0 {
		out += "_none_\n"
	}
	for _, l := range report.Losses {
		out += fmt.Sprintf("- %q\n", l.Query)
	}
	return out
}

func renderCorrection(report discoveryEval.CorrectionReport) string {
	out := fmt.Sprintf("# Discovery correction eval — %s\n\n", time.Now().UTC().Format(time.RFC3339))
	out += fmt.Sprintf("- Terms (known-good): %d\n", report.Terms)
	out += fmt.Sprintf("- Recall: %.1f%% (%d of %d typos recovered)\n", report.RecallRate()*100, report.Recovered, report.TyposTested)
	out += fmt.Sprintf("- Precision: %.1f%% (%d valid queries corrupted)\n", report.PrecisionRate()*100, report.Corrupted)

	if len(report.Corruptions) > 0 {
		out += "\n## Corruptions (valid query rewritten — the costly failure)\n\n"
		for _, c := range report.Corruptions {
			out += fmt.Sprintf("- %q → %v\n", c.Query, c.Attrs["corrected_to"])
		}
	}
	return out
}

func renderSignalA(report *discoveryEval.CoverageReportA, sinceDays int) string {
	out := fmt.Sprintf("# Discovery coverage signal A — %s\n\n", time.Now().UTC().Format(time.RFC3339))
	out += fmt.Sprintf("Window: last %d days. Strong = zero-result (not a typo); weak = results shown but no click.\n\n", sinceDays)

	out += fmt.Sprintf("## Strong gaps — %d (filtered %d typos)\n\n", len(report.Strong), report.FilteredAsTypos)
	out += renderGaps(report.Strong)

	out += fmt.Sprintf("\n## Weak hints — %d\n\n", len(report.Weak))
	out += renderGaps(report.Weak)
	return out
}

func renderSignalB(report *discoveryEval.CoverageReportB) string {
	out := fmt.Sprintf("# Discovery coverage signal B — %s\n\n", time.Now().UTC().Format(time.RFC3339))
	out += fmt.Sprintf("Artists scanned: %d. Total entities (union): %d.\n\n", report.ArtistsScanned, report.TotalEntities)

	out += "## Provider imbalance\n\n"
	if len(report.ProviderGaps) == 0 {
		out += "_none_\n"
	}
	for _, g := range report.ProviderGaps {
		covered := g.Union - g.Missing
		out += fmt.Sprintf("- %s: covered %d / %d (%.1f%% gap) — unique reach %d\n",
			g.Provider, covered, g.Union, g.GapPct*100, g.Unique)
	}

	out += "\n## Caveats\n\n"
	for _, c := range report.Caveats {
		out += fmt.Sprintf("- %s\n", c)
	}
	return out
}

func renderGaps(gaps []discoveryEval.CoverageGap) string {
	if len(gaps) == 0 {
		return "_none_\n"
	}
	out := ""
	for _, g := range gaps {
		out += fmt.Sprintf("- %q — %d×\n", g.QueryNorm, g.Count)
	}
	return out
}
