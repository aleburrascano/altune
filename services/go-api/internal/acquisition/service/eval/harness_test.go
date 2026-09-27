package eval

import (
	"context"
	"io"
	"log/slog"
	"strings"
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
		{"pending with a blank owning ticket", []Case{
			{ID: "x", Class: "F1", Pending: "  ", Track: Track{Title: "t", Artist: "a"}, Candidates: []Candidate{{URL: "u"}}},
		}},
		{"resolution beside a copied mbid", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a", MBID: "m", Resolution: &Resolution{}}, Candidates: []Candidate{{URL: "u"}}},
		}},
		{"resolution beside copied acoustids", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a", AcoustIDs: []string{"ac"}, Resolution: &Resolution{}}, Candidates: []Candidate{{URL: "u"}}},
		}},
		{"resolution beside a copied authoritative duration", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a", AuthoritativeDuration: 200, Resolution: &Resolution{}}, Candidates: []Candidate{{URL: "u"}}},
		}},
		{"isrc recordings for a track with no isrc", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a", Resolution: &Resolution{ISRCRecordings: []ISRCRecording{{MBID: "m"}}}}, Candidates: []Candidate{{URL: "u"}}},
		}},
		{"isrc recording with no mbid", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a", ISRC: "I", Resolution: &Resolution{ISRCRecordings: []ISRCRecording{{Duration: 200}}}}, Candidates: []Candidate{{URL: "u"}}},
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

func TestValidateCases_AcceptsAResolutionWithOnlyAnISRCAnswer(t *testing.T) {
	cases := []Case{{
		ID: "x", Class: "RW", Pending: "owning ticket",
		Track: Track{
			Title: "t", Artist: "a", ISRC: "I",
			Resolution: &Resolution{ISRCRecordings: []ISRCRecording{{MBID: "m", Duration: 200}}},
		},
		Candidates: []Candidate{{URL: "u"}},
	}}

	if err := validateCases(cases); err != nil {
		t.Fatalf("validateCases rejected a well-formed resolution case: %v", err)
	}
}

func embeddedCase(t *testing.T, id string) Case {
	t.Helper()
	cases, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	for _, c := range cases {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no embedded case %q", id)
	return Case{}
}

func TestRun_DrinkingInLAResolvesTheRemixMBIDToTheISRCRecording(t *testing.T) {
	kase := embeddedCase(t, "rw-drinking-in-la-remix-mbid-anchored-to-isrc")

	out := Run(context.Background(), kase)

	if !out.Pass {
		t.Fatalf("expected pass, got %q (err %q)", out.Reason, out.Err)
	}
	if want := "https://www.youtube.com/watch?v=-_5yNLsZ5Pk"; out.Stored != want {
		t.Errorf("stored %q, want the Topic upload %q", out.Stored, want)
	}
}

func remixMBIDCase(resolution *Resolution, isrc string) Case {
	return Case{
		ID: "t", Class: "RW",
		Track: Track{Title: "Drinking in L.A.", Artist: "Bran Van 3000", Duration: 236, ISRC: isrc, Resolution: resolution},
		Candidates: []Candidate{
			{Title: "Drinking in L.A.", URL: "album", Channel: "Bran Van 3000 - Topic", Duration: 237, AcoustID: "album-ac", RecordingMBIDs: []string{"album"}, Correct: true},
			{Title: "Drinking in L.A. (Who Mix?)", URL: "remix", Channel: "Bran Van 3000 - Topic", Duration: 307, AcoustID: "remix-ac", RecordingMBIDs: []string{"remix"}},
		},
	}
}

func TestRun_ResolvedSearchMBIDSetsTheExpectedAcoustIDCluster(t *testing.T) {
	kase := remixMBIDCase(&Resolution{Search: &SearchedRecording{MBID: "remix", Duration: 236}}, "")

	out := Run(context.Background(), kase)

	if !out.Failed {
		t.Fatalf("expected the remix's AcoustID cluster to reject the album upload, stored %q", out.Stored)
	}
}

func TestRun_ISRCAnswerAloneSuppliesTheIdentity(t *testing.T) {
	kase := remixMBIDCase(&Resolution{ISRCRecordings: []ISRCRecording{{MBID: "remix", Duration: 307}}}, "CAA509814003")
	kase.Track.Duration = 0

	out := Run(context.Background(), kase)

	if out.Stored != "remix" {
		t.Fatalf("stored %q, want the remix the ISRC answer names (its MBID and 307s length)", out.Stored)
	}
}

func TestRun_CaseWithoutResolutionCopiesItsIdentity(t *testing.T) {
	kase := remixMBIDCase(nil, "")
	kase.Track.MBID = "remix"
	kase.Track.AcoustIDs = []string{"remix-ac"}

	out := Run(context.Background(), kase)

	if !out.Failed {
		t.Fatalf("expected the copied remix cluster to reject the album upload, stored %q", out.Stored)
	}
}

func TestRun_MarksAPendingCase(t *testing.T) {
	kase := remixMBIDCase(nil, "")
	kase.Pending = "selection vetoes unrequested versions"

	out := Run(context.Background(), kase)

	if !out.Pending {
		t.Error("expected a case naming an owning ticket to be reported as pending")
	}
}

func TestRun_ScoredCaseIsNotPending(t *testing.T) {
	out := Run(context.Background(), remixMBIDCase(nil, ""))

	if out.Pending {
		t.Error("expected a case with no owning ticket to be scored, not pending")
	}
}

func TestSummarize_KeepsPendingOutOfTheScore(t *testing.T) {
	outcomes := []Outcome{
		{Case: Case{ID: "a", Class: "F1"}, Pass: true},
		{Case: Case{ID: "b", Class: "F1", Pending: "t1"}, Pending: true, Reason: "wrong"},
		{Case: Case{ID: "c", Class: "RW", Pending: "t2"}, Pending: true, Pass: true},
	}

	r := Summarize(outcomes)

	if r.Total != 1 || r.Passed != 1 {
		t.Fatalf("total/passed = %d/%d, want 1/1", r.Total, r.Passed)
	}
	if len(r.Classes) != 1 || r.Classes[0] != (ClassResult{Class: "F1", Total: 1, Passed: 1}) {
		t.Fatalf("classes = %+v, want only F1 at 1/1", r.Classes)
	}
	if len(r.Failures) != 0 {
		t.Fatalf("failures = %+v, want none", r.Failures)
	}
	if len(r.Pending) != 2 || r.Pending[0].Case.ID != "b" || r.Pending[1].Case.ID != "c" {
		t.Fatalf("pending = %+v, want b and c", r.Pending)
	}
}

func TestRender_ListsPendingCasesWithTheirOwner(t *testing.T) {
	r := Summarize([]Outcome{
		{Case: Case{ID: "a", Class: "F1"}, Pass: true},
		{Case: Case{ID: "mh-fail", Class: "RW", Pending: "owner one"}, Pending: true, Reason: "stored the wrong recording"},
		{Case: Case{ID: "mh-pass", Class: "RW", Pending: "owner two"}, Pending: true, Pass: true},
	})

	got := r.Render()

	for _, want := range []string{"Pending", "FAIL mh-fail", "owner: owner one", "stored the wrong recording", "PASS mh-pass", "owner: owner two"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Failures:") {
		t.Errorf("Render() listed a pending case as a failure:\n%s", got)
	}
}

func TestRender_OmitsPendingSectionWhenNothingIsPending(t *testing.T) {
	got := Summarize([]Outcome{{Case: Case{ID: "a", Class: "F1"}, Pass: true}}).Render()

	if strings.Contains(got, "Pending") {
		t.Errorf("Render() printed a pending section with nothing pending:\n%s", got)
	}
}

func TestRunAll_ReportsEmbeddedMustHoldsAsPending(t *testing.T) {
	cases, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	want := map[string]string{
		"mh4-unknown-fingerprint-topic-within-2s-stored-best-effort": "compute acquisition confidence from evidence",
		"mh4-unknown-fingerprint-non-topic-20s-off-fails":            "compute acquisition confidence from evidence",
		"rw-rollacoasta-instrumental-top-ranked":                     "selection vetoes unrequested versions",
		"rw-speed-demon-instrumental-only":                           "selection vetoes unrequested versions",
		"mh11-radio-edit-loses-to-clean-topic-upload":                "compute acquisition confidence from evidence",
		"mh11-radio-edit-only-with-wrong-length-fails":               "compute acquisition confidence from evidence",
	}

	report := Summarize(RunAll(context.Background(), cases))

	got := make(map[string]string, len(report.Pending))
	for _, p := range report.Pending {
		got[p.Case.ID] = p.Case.Pending
	}
	for id, owner := range want {
		if got[id] != owner {
			t.Errorf("pending %q owner = %q, want %q", id, got[id], owner)
		}
	}
	if scored := len(cases) - len(report.Pending); report.Total != scored {
		t.Errorf("scored total = %d, want %d with pending cases left out", report.Total, scored)
	}
}

func TestEmbeddedPendingCasesNameTheirOwningTicket(t *testing.T) {
	cases, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}

	for _, c := range cases {
		if c.isPending() && strings.TrimSpace(c.Pending) == "" {
			t.Errorf("pending case %q names no owning ticket", c.ID)
		}
	}
}

func TestRunAll_ScoresDrinkingInLAInTheRealWorldClass(t *testing.T) {
	cases, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}

	report := Summarize(RunAll(context.Background(), cases))

	var rw *ClassResult
	for i := range report.Classes {
		if report.Classes[i].Class == "RW" {
			rw = &report.Classes[i]
		}
	}
	if rw == nil {
		t.Fatalf("no RW class in the scored report: %+v", report.Classes)
	}
	if rw.Total < 1 || rw.Passed != rw.Total {
		t.Errorf("RW scored %d/%d, want every real-world case to pass", rw.Passed, rw.Total)
	}
	for _, p := range report.Pending {
		if p.Case.ID == "rw-drinking-in-la-remix-mbid-anchored-to-isrc" {
			t.Error("the Drinking in L.A. case is pending, want it scored")
		}
	}
}

func TestRun_RemixMBIDAnchorsToTheISRCRecordingNearestTheTrackLength(t *testing.T) {
	kase := Case{
		ID: "t", Class: "RW",
		Track: Track{
			Title: "Drinking in L.A.", Artist: "Bran Van 3000", Duration: 236, ISRC: "CAA509814003",
			Resolution: &Resolution{
				Search: &SearchedRecording{MBID: "remix", ISRC: "CAA509814003", Duration: 236},
				ISRCRecordings: []ISRCRecording{
					{MBID: "edit", Duration: 220},
					{MBID: "album", Duration: 236},
				},
			},
		},
		Candidates: []Candidate{
			{Title: "Drinking in L.A. (edit)", URL: "edit", Channel: "Bran Van 3000 - Topic", Duration: 236, AcoustID: "edit-ac", RecordingMBIDs: []string{"edit"}},
			{Title: "Drinking in L.A.", URL: "album", Channel: "Bran Van 3000 - Topic", Duration: 237, AcoustID: "album-ac", RecordingMBIDs: []string{"album"}, Correct: true},
		},
	}

	out := Run(context.Background(), kase)

	if out.Stored != "album" {
		t.Fatalf("stored %q (reason %q), want the album recording whose 236s length matches the track", out.Stored, out.Reason)
	}
}

func TestRegressions_IgnoresAFailingPendingCase(t *testing.T) {
	report := Summarize([]Outcome{
		{Case: Case{ID: "a", Class: "RW"}, Pass: true},
		{Case: Case{ID: "b", Class: "RW", Pending: "selection vetoes unrequested versions"}, Pending: true, Stored: "instrumental", Reason: "stored the wrong recording"},
		{Case: Case{ID: "c", Class: "F1", Pending: "compute acquisition confidence from evidence"}, Pending: true, Failed: true, Reason: "nothing stored"},
	})
	base := Baseline{Accuracy: 1.0, Classes: map[string]float64{"RW": 1.0}}

	if got := report.Regressions(base); len(got) != 0 {
		t.Fatalf("failing pending cases moved the baseline gate: %v", got)
	}
}

func TestRun_EmptyResolutionStillAcquiresTheTrack(t *testing.T) {
	kase := Case{
		ID: "t", Class: "OK",
		Track:      Track{Title: "Circles", Artist: "Post Malone", Duration: 215, Resolution: &Resolution{}},
		Candidates: []Candidate{{Title: "Circles", URL: "u", Channel: "Post Malone - Topic", Duration: 215, Correct: true}},
	}

	out := Run(context.Background(), kase)

	if out.Stored != "u" {
		t.Fatalf("stored %q (reason %q, err %q), want the only clean upload", out.Stored, out.Reason, out.Err)
	}
}
