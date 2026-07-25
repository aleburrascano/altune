package main

import (
	"path/filepath"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
	discoveryEval "altune/go-api/internal/discovery/service/eval"
)

func TestMetrics_WriteLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()

	err := maybeWriteMetrics(filepath.Join(dir, "metrics-eval.json"), "eval", []discoveryEval.NamedMetric{
		{Name: "eval.top1_rate", Value: 0.98, HigherIsBetter: true},
	})
	if err != nil {
		t.Fatalf("maybeWriteMetrics: %v", err)
	}
	err = maybeWriteMetrics(filepath.Join(dir, "metrics-merge.json"), "merge", []discoveryEval.NamedMetric{
		{Name: "merge.over_merge_rate", Value: 0.001, HigherIsBetter: false},
	})
	if err != nil {
		t.Fatalf("maybeWriteMetrics: %v", err)
	}

	files, err := loadMetricsFiles(dir)
	if err != nil {
		t.Fatalf("loadMetricsFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("loaded %d files, want 2", len(files))
	}
	if files[0].Mode != "eval" || files[0].Metrics[0].Name != "eval.top1_rate" {
		t.Errorf("first file = %+v, want the eval metrics", files[0])
	}
}

func TestMaybeWriteMetrics_EmptyPathIsNoop(t *testing.T) {
	if err := maybeWriteMetrics("", "eval", nil); err != nil {
		t.Errorf("expected a no-op for an empty path, got %v", err)
	}
}

func TestLoadMetricsFiles_EmptyDirErrors(t *testing.T) {
	if _, err := loadMetricsFiles(t.TempDir()); err == nil {
		t.Error("expected an error when no metrics files are present")
	}
}

func TestRenderReport_FlagsRegressionAndDelta(t *testing.T) {
	gates := []discoveryEval.GateResult{
		{Metric: "eval.top1_rate", Current: 0.97, Baseline: 0.985, Margin: 0.002, Regressed: true},
		{Metric: "eval.topk_rate", Current: 0.996, Baseline: 0.995, Margin: 0.002},
	}
	out := renderReport([]string{"eval"}, map[string][]discoveryEval.GateResult{"eval": gates}, gates[:1])

	if !strings.Contains(out, "**1 metric(s) regressed**") {
		t.Errorf("missing regression headline:\n%s", out)
	}
	if !strings.Contains(out, "-0.0150") {
		t.Errorf("missing signed delta for the regressed metric:\n%s", out)
	}
	if !strings.Contains(out, "+0.0010") {
		t.Errorf("missing signed delta for the improved metric:\n%s", out)
	}
	if !strings.Contains(out, "| eval | `eval.topk_rate` | 0.9960 | 0.9950 | ±0.0020 | +0.0010 | ok |") {
		t.Errorf("missing the ok row:\n%s", out)
	}
}

func TestRenderReport_CleanRunSaysSo(t *testing.T) {
	gates := []discoveryEval.GateResult{{Metric: "detail.contamination", Current: 0, Baseline: 0}}
	out := renderReport([]string{"detail"}, map[string][]discoveryEval.GateResult{"detail": gates}, nil)

	if !strings.Contains(out, "**No regressions** across 1 mode(s).") {
		t.Errorf("missing clean-run headline:\n%s", out)
	}
}

func TestRenderReport_MissingBaselineRendersAsNew(t *testing.T) {
	gates := []discoveryEval.GateResult{{Metric: "eval.new_metric", Current: 0.5, Missing: true}}
	out := renderReport([]string{"eval"}, map[string][]discoveryEval.GateResult{"eval": gates}, nil)

	if !strings.Contains(out, "| new |") {
		t.Errorf("a metric with no baseline should render as new:\n%s", out)
	}
}

func TestRegressionDigest_NamesEachMetricWithItsDelta(t *testing.T) {
	digest := regressionDigest([]discoveryEval.GateResult{
		{Metric: "eval.top1_rate", Current: 0.97, Baseline: 0.985},
		{Metric: "merge.collapse_rate", Current: 0.04, Baseline: 0.053},
	})
	want := "eval.top1_rate -0.0150, merge.collapse_rate -0.0130"
	if digest != want {
		t.Errorf("digest = %q, want %q", digest, want)
	}
}

func TestSeedEntries_KindsTermsAndDedupes(t *testing.T) {
	entries := seedEntries([]discoveryEval.LibraryEntity{
		{Artist: "Kendrick Lamar", Title: "Alright"},
		{Artist: "Kendrick Lamar", Title: "DNA."},
		{Artist: "", Title: "Orphan"},
	})

	if len(entries) != 4 {
		t.Fatalf("seeded %d entries, want 4 (one artist, three titles, empty dropped)", len(entries))
	}
	artists, tracks := 0, 0
	for _, e := range entries {
		switch e.Kind {
		case domain.VocabKindArtist:
			artists++
		case domain.VocabKindTrack:
			tracks++
		}
		if e.Term == "" {
			t.Error("empty term seeded")
		}
	}
	if artists != 1 || tracks != 3 {
		t.Errorf("kinds = %d artist / %d track, want 1 / 3", artists, tracks)
	}
}
