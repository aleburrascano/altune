package main

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	discoveryEval "altune/go-api/internal/discovery/service/eval"
	goldeneval "altune/go-api/internal/discovery/service/goldeneval"
)

func runWithBaselines(t *testing.T, baselines discoveryEval.Baselines) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "baselines.json")
	if err := writeBaselines(path, baselines); err != nil {
		t.Fatalf("writeBaselines: %v", err)
	}
	once := func() (discoveryEval.HarnessReport, error) { return goldeneval.Report{}, nil }
	human := func(discoveryEval.HarnessReport) string { return "" }
	return runHarness("golden", once, human, options{baselinesPath: path})
}

func TestRunHarness_AMetricWithNoBaselineFailsTheRun(t *testing.T) {
	err := runWithBaselines(t, discoveryEval.Baselines{})

	if !errors.Is(err, errRegressed) {
		t.Fatalf("runHarness = %v, want errRegressed for metrics that have no baseline", err)
	}
}

func TestRunHarness_AMissingBaselinesFileFailsTheRun(t *testing.T) {
	once := func() (discoveryEval.HarnessReport, error) { return goldeneval.Report{}, nil }
	human := func(discoveryEval.HarnessReport) string { return "" }
	missing := filepath.Join(t.TempDir(), "absent.json")

	err := runHarness("golden", once, human, options{baselinesPath: missing})

	if !errors.Is(err, errRegressed) {
		t.Fatalf("runHarness = %v, want errRegressed when no baselines file exists", err)
	}
	if _, statErr := os.Stat(missing); statErr == nil {
		t.Error("a gated run must not create the baselines file")
	}
}

func TestRunHarness_BaselinedMetricsPass(t *testing.T) {
	baselines := discoveryEval.BuildBaselines(goldeneval.Report{}.Metrics(), nil)

	if err := runWithBaselines(t, baselines); err != nil {
		t.Fatalf("runHarness = %v, want a pass when every metric sits on its baseline", err)
	}
}

func nightlyMetricNames() []string {
	reports := []discoveryEval.HarnessReport{
		discoveryEval.EvalReport{},
		discoveryEval.MergeReport{},
		discoveryEval.DiversityReport{},
		discoveryEval.ArtistIntentReport{},
		discoveryEval.CorrectionReport{},
		discoveryEval.DetailReport{},
		&discoveryEval.CoverageReportA{},
		&discoveryEval.CoverageReportB{},
		goldeneval.Report{},
	}
	names := []string{}
	for _, report := range reports {
		for _, metric := range report.Metrics() {
			names = append(names, metric.Name)
		}
	}
	return names
}

func TestCommittedBaselines_CoverEveryMetricTheNightlyEmits(t *testing.T) {
	baselines, err := loadBaselines("baselines.json")
	if err != nil {
		t.Fatalf("loadBaselines: %v", err)
	}

	for _, name := range nightlyMetricNames() {
		if _, ok := baselines[name]; !ok {
			t.Errorf("metric %q is emitted by the nightly but has no committed baseline", name)
		}
	}
}

func TestCommittedBaselines_HoldNoMetricNoModeEmits(t *testing.T) {
	baselines, err := loadBaselines("baselines.json")
	if err != nil {
		t.Fatalf("loadBaselines: %v", err)
	}
	emitted := map[string]bool{}
	for _, name := range nightlyMetricNames() {
		emitted[name] = true
	}
	for _, report := range []discoveryEval.HarnessReport{
		discoveryEval.EvalReport{Corpus: "hard"},
		discoveryEval.DiversityReport{Corpus: "hard"},
		discoveryEval.ArtistIntentReport{Corpus: "hard"},
	} {
		for _, metric := range report.Metrics() {
			emitted[metric.Name] = true
		}
	}

	for name := range baselines {
		if !emitted[name] {
			t.Errorf("baseline %q is gated by no mode: no code emits that metric", name)
		}
	}
}

type failingSearcher struct{}

func (failingSearcher) Search(context.Context, string) ([]domain.SearchResult, error) {
	return nil, errors.New("provider down")
}

type silentVariantSearcher struct{}

func (silentVariantSearcher) SearchVariants(context.Context, string) ([]domain.SearchResult, []domain.SearchResult) {
	return nil, nil
}

func TestMeasureMerge_FailsWhenEverySearchErrors(t *testing.T) {
	entities := []discoveryEval.LibraryEntity{{Title: "Humble", Artist: "Kendrick"}}

	_, err := measureMerge(context.Background(), entities, failingSearcher{}, 1, nil)

	if err == nil {
		t.Fatal("measureMerge = nil, want an error when no search answered")
	}
}

func TestMeasureDiversity_FailsWhenNothingAnswers(t *testing.T) {
	entities := []discoveryEval.LibraryEntity{{Title: "Humble", Artist: "Kendrick"}}

	_, err := measureDiversity(context.Background(), entities, silentVariantSearcher{}, 1, 10, discoveryEval.QueryMode(0), nil)

	if err == nil {
		t.Fatal("measureDiversity = nil, want an error when no query was answered")
	}
}

func TestRequireSignalASamples_FailsOnAnEmptyReport(t *testing.T) {
	if err := requireSignalASamples(&discoveryEval.CoverageReportA{}); err == nil {
		t.Fatal("requireSignalASamples = nil, want an error for a report with no samples")
	}
}

func TestRequireMeasured_SignalBWithNoArtistsFails(t *testing.T) {
	if err := requireMeasured("signal-b", 0, 0); err == nil {
		t.Fatal("requireMeasured = nil, want an error for zero samples")
	}
}
