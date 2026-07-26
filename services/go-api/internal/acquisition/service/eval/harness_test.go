package eval

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.Run()
}

func TestLoadEmbedded_ValidatesAndSorts(t *testing.T) {
	cases, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("expected embedded golden cases")
	}
	for i := 1; i < len(cases); i++ {
		if cases[i-1].ID > cases[i].ID {
			t.Fatalf("cases are not sorted by id: %q before %q", cases[i-1].ID, cases[i].ID)
		}
	}
}

func TestValidateCases_RejectsMalformed(t *testing.T) {
	tests := []struct {
		name  string
		cases []Case
	}{
		{"no id", []Case{{Class: "F1", Track: Track{Title: "t", Artist: "a"}, Candidates: []Candidate{{URL: "u"}}}}},
		{"no class", []Case{{ID: "x", Track: Track{Title: "t", Artist: "a"}, Candidates: []Candidate{{URL: "u"}}}}},
		{"no artist", []Case{{ID: "x", Class: "F1", Track: Track{Title: "t"}, Candidates: []Candidate{{URL: "u"}}}}},
		{"no candidates", []Case{{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a"}}}},
		{"candidate without url", []Case{{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Candidates: []Candidate{{Title: "c"}}}}},
		{"duplicate id", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Candidates: []Candidate{{URL: "u"}}},
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Candidates: []Candidate{{URL: "u"}}},
		}},
		{"duplicate candidate url", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Candidates: []Candidate{{URL: "u"}, {URL: "u"}}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateCases(tt.cases); err == nil {
				t.Error("expected validation to reject this suite")
			}
		})
	}
}

func TestRun_StoresTheCorrectRecording(t *testing.T) {
	kase := Case{
		ID: "t", Class: "OK",
		Track: Track{Title: "Blinding Lights", Artist: "The Weeknd", Duration: 200},
		Candidates: []Candidate{
			{Title: "Blinding Lights (Slowed)", URL: "slowed", Channel: "The Weeknd - Topic", Duration: 240},
			{Title: "Blinding Lights", URL: "master", Channel: "The Weeknd - Topic", Duration: 200, Correct: true},
		},
	}
	out := Run(context.Background(), kase)
	if !out.Pass {
		t.Fatalf("expected pass, got %q (stored %q)", out.Reason, out.Stored)
	}
	if out.Stored != "master" {
		t.Errorf("stored %q, want master", out.Stored)
	}
}

func TestRun_FailsWhenWrongRecordingStored(t *testing.T) {
	kase := Case{
		ID: "t", Class: "F1",
		Track:      Track{Title: "Blinding Lights", Artist: "The Weeknd", Duration: 200},
		Candidates: []Candidate{{Title: "Blinding Lights (Cover)", URL: "cover", Channel: "Covers", Duration: 200}},
	}
	out := Run(context.Background(), kase)
	if out.Pass {
		t.Fatal("expected a fail: no candidate was the right recording")
	}
	if out.Stored != "cover" {
		t.Errorf("expected the harness to record what was wrongly stored, got %q", out.Stored)
	}
}

func TestRun_PassesWhenNothingCorrectAndNothingStored(t *testing.T) {
	kase := Case{
		ID: "t", Class: "F7",
		Track:      Track{Title: "Save Your Tears", Artist: "The Weeknd", Duration: 215},
		Candidates: []Candidate{{Title: "Cooking Tutorial Episode 47", URL: "cook", Channel: "Cooking", Duration: 215}},
	}
	out := Run(context.Background(), kase)
	if !out.Pass {
		t.Fatalf("expected pass, got %q", out.Reason)
	}
	if !out.Failed {
		t.Error("expected the pipeline to reject every candidate")
	}
}

func TestRun_LeavesNoTempFiles(t *testing.T) {
	kase := Case{
		ID: "t", Class: "OK",
		Track:      Track{Title: "Circles", Artist: "Post Malone", Duration: 215},
		Candidates: []Candidate{{Title: "Circles", URL: "u", Channel: "Post Malone - Topic", Duration: 215, Correct: true}},
	}
	out := Run(context.Background(), kase)
	if !out.Pass {
		t.Fatalf("expected pass, got %q", out.Reason)
	}
}

func TestSummarize_GroupsByClassAndCollectsFailures(t *testing.T) {
	outcomes := []Outcome{
		{Case: Case{ID: "a", Class: "F1"}, Pass: true},
		{Case: Case{ID: "b", Class: "F1"}, Pass: false, Reason: "wrong"},
		{Case: Case{ID: "c", Class: "F2"}, Pass: true},
	}
	r := Summarize(outcomes)

	if r.Total != 3 || r.Passed != 2 {
		t.Fatalf("total/passed = %d/%d, want 3/2", r.Total, r.Passed)
	}
	if len(r.Classes) != 2 || r.Classes[0].Class != "F1" || r.Classes[0].Accuracy() != 0.5 {
		t.Fatalf("classes = %+v", r.Classes)
	}
	if len(r.Failures) != 1 || r.Failures[0].Case.ID != "b" {
		t.Fatalf("failures = %+v", r.Failures)
	}
}

func TestRegressions_FlagsOverallAndPerClassDrops(t *testing.T) {
	r := Report{Total: 2, Passed: 1, Classes: []ClassResult{{Class: "F1", Total: 2, Passed: 1}}}
	base := Baseline{Accuracy: 1.0, Classes: map[string]float64{"F1": 1.0}}

	got := r.Regressions(base)
	if len(got) != 2 {
		t.Fatalf("expected an overall and a per-class regression, got %v", got)
	}
}

func TestRegressions_SilentWhenAtBaseline(t *testing.T) {
	r := Report{Total: 2, Passed: 2, Classes: []ClassResult{{Class: "F1", Total: 2, Passed: 2}}}
	base := Baseline{Accuracy: 1.0, Classes: map[string]float64{"F1": 1.0}}

	if got := r.Regressions(base); len(got) != 0 {
		t.Fatalf("expected no regressions, got %v", got)
	}
}

func TestEmbeddedSuiteMatchesCommittedBaseline(t *testing.T) {
	cases, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	base, err := LoadBaseline("../../../../cmd/acquisitioneval/baselines.json")
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}
	report := Summarize(RunAll(context.Background(), cases))

	if regressions := report.Regressions(base); len(regressions) > 0 {
		t.Errorf("embedded suite regressed against the committed baseline: %v", regressions)
	}
}
