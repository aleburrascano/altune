package eval

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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
		{"candidates and sources both set", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Candidates: []Candidate{{URL: "u"}}, Sources: []Source{{Name: "s", Candidates: []Candidate{{URL: "v"}}}}},
		}},
		{"source with no name", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Sources: []Source{{Candidates: []Candidate{{URL: "u"}}}}},
		}},
		{"source with no candidates and no simulated failure", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Sources: []Source{{Name: "s"}}},
		}},
		{"candidate with an unknown query variant", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Candidates: []Candidate{{URL: "u", Query: "bogus"}}},
		}},
		{"candidate title with a terminal escape", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Candidates: []Candidate{{URL: "u", Title: "Halo \x1b[31m"}}},
		}},
		{"candidate channel with a bidi override", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Candidates: []Candidate{{URL: "u", Channel: "Topic ‮"}}},
		}},
		{"track title with a bell", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t\x07", Artist: "a"}, Candidates: []Candidate{{URL: "u"}}},
		}},
		{"track artist with a C1 control", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a\u009b"}, Candidates: []Candidate{{URL: "u"}}},
		}},
		{"track album with a bidi isolate", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a", Album: "⁦b"}, Candidates: []Candidate{{URL: "u"}}},
		}},
		{"named-source candidate title with a delete", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Sources: []Source{{Name: "s", Candidates: []Candidate{{URL: "u", Title: "c\x7f"}}}}},
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

func TestValidateCases_NamesTheCaseAndFieldHoldingAnUnsafeRune(t *testing.T) {
	tests := []struct {
		name      string
		kase      Case
		wantField string
	}{
		{"candidate title escape", Case{ID: "halo-esc", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Candidates: []Candidate{{URL: "u", Title: "Halo \x1b[31m"}}}, `candidate "u" title`},
		{"candidate channel bidi", Case{ID: "halo-bidi", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Candidates: []Candidate{{URL: "u", Channel: "Topic ‮"}}}, `candidate "u" channel`},
		{"track title bell", Case{ID: "halo-bell", Class: "F1", Track: Track{Title: "t\x07", Artist: "a"}, Candidates: []Candidate{{URL: "u"}}}, "track title"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCases([]Case{tt.kase})
			if err == nil {
				t.Fatal("expected validation to reject this suite")
			}
			msg := err.Error()
			if !strings.Contains(msg, `"`+tt.kase.ID+`"`) || !strings.Contains(msg, tt.wantField) {
				t.Errorf("error %q should name case %q and field %q", msg, tt.kase.ID, tt.wantField)
			}
			if strings.ContainsFunc(msg, func(r rune) bool { return r == '\x1b' || r == '‮' || r == '\x07' }) {
				t.Errorf("error %q echoes the unsafe rune raw", msg)
			}
		})
	}
}

func TestValidateCases_AcceptsOrdinaryUnicodeText(t *testing.T) {
	kase := Case{
		ID: "beyonce-halo", Class: "OK",
		Track: Track{Title: "Halo", Artist: "Beyoncé", Album: "I Am... Sasha Fierce"},
		Candidates: []Candidate{
			{URL: "u", Title: "Beyoncé – Halo (Official Video) 🎵", Channel: "ビヨンセ · Topic"},
			{URL: "v", Title: "هالو – بيونسيه", Channel: "Beyoncé VEVO"},
		},
	}
	if err := validateCases([]Case{kase}); err != nil {
		t.Fatalf("expected ordinary unicode to pass, got %v", err)
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
		Candidates: []Candidate{{Title: "Blinding Lights", URL: "cover", Channel: "Covers", Duration: 200}},
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
			{Title: "Drinking in L.A. (Who?)", URL: "remix", Channel: "Bran Van 3000 - Topic", Duration: 307, AcoustID: "remix-ac", RecordingMBIDs: []string{"remix"}},
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

func TestRunAll_ScoresEmbeddedMustHoldsInsteadOfLeavingThemPending(t *testing.T) {
	cases, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	want := map[string]bool{
		"mh4-unknown-fingerprint-topic-within-2s-stored-best-effort": true,
		"mh4-unknown-fingerprint-non-topic-20s-off-fails":            true,
		"mh11-radio-edit-loses-to-clean-topic-upload":                true,
		"mh11-radio-edit-only-with-wrong-length-fails":               true,
	}

	for _, o := range RunAll(context.Background(), cases) {
		if !want[o.Case.ID] {
			continue
		}
		delete(want, o.Case.ID)
		if o.Pending || !o.Pass {
			t.Errorf("%s: pending=%v pass=%v reason=%q", o.Case.ID, o.Pending, o.Pass, o.Reason)
		}
	}
	for id := range want {
		t.Errorf("must-hold case %q was not run", id)
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

func TestRun_TwoNamedSourcesMergeThroughTheRealRegistry(t *testing.T) {
	kase := Case{
		ID: "t", Class: "OK",
		Track: Track{Title: "Solitude", Artist: "Nova", Duration: 200},
		Sources: []Source{
			{Name: "ytmusic", Candidates: []Candidate{
				{Title: "Solitude (Cover)", URL: "cover", Channel: "Cover Channel", Duration: 200},
			}},
			{Name: "ytdlp", Candidates: []Candidate{
				{Title: "Solitude", URL: "master", Channel: "Nova - Topic", Duration: 200, Correct: true},
			}},
		},
	}

	out := Run(context.Background(), kase)

	if !out.Pass {
		t.Fatalf("expected pass, got %q (stored %q)", out.Reason, out.Stored)
	}
	if out.Stored != "master" {
		t.Errorf("stored %q, want the ytdlp source's master over ytmusic's cover", out.Stored)
	}
}

func TestRun_CandidateBehindAnAbsentQueryVariantNeverBecomesACandidate(t *testing.T) {
	kase := Case{
		ID: "t", Class: "OK",
		Track: Track{Title: "Quiet Static", Artist: "Nova", Duration: 190},
		Sources: []Source{{Name: "ytdlp", Candidates: []Candidate{
			{Title: "Quiet Static", URL: "album-edition", Channel: "Nova - Topic", Duration: 190, ViewCount: 900000, Query: QueryTitleArtistAlbum},
			{Title: "Quiet Static", URL: "master", Channel: "Nova - Topic", Duration: 190, ViewCount: 100, Query: QueryTitleArtist, Correct: true},
		}}},
	}

	out := Run(context.Background(), kase)

	if !out.Pass {
		t.Fatalf("expected pass, got %q (stored %q): the title_artist_album candidate must never surface without an album on the track", out.Reason, out.Stored)
	}
	if out.Stored != "master" {
		t.Errorf("stored %q, want master: the higher-view candidate behind an absent query variant must never be found", out.Stored)
	}
}

func TestEvalSource_FindsCandidateOnlyForItsQueryVariant(t *testing.T) {
	kase := Case{
		ID: "t", Class: "OK",
		Track: Track{Title: "Solitude", Artist: "Nova", Duration: 200},
		Sources: []Source{{Name: "ytdlp", Candidates: []Candidate{
			{Title: "Solitude (Deluxe)", URL: "album-edition", Query: QueryTitleArtistAlbum},
			{Title: "Solitude", URL: "master", Query: QueryTitleArtist, Correct: true},
		}}},
	}
	source := newCasePorts(kase).sources()[0]

	withoutAlbum, err := source.Find(context.Background(), ports.FindRequest{Title: "Solitude", Artist: "Nova"})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(withoutAlbum) != 1 || withoutAlbum[0].URL != "master" {
		t.Fatalf("Find without an album = %+v, want only the title_artist candidate", withoutAlbum)
	}

	withAlbum, err := source.Find(context.Background(), ports.FindRequest{Title: "Solitude", Artist: "Nova", Album: "Nightfall"})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(withAlbum) != 2 {
		t.Fatalf("Find with an album = %+v, want both candidates", withAlbum)
	}
}

func TestRun_SimulatedSecondsAndAttemptsAccumulateAlongThePath(t *testing.T) {
	kase := Case{
		ID: "t", Class: "OK",
		Track: Track{Title: "Solitude", Artist: "Nova", Duration: 200},
		Sources: []Source{{Name: "ytdlp", SearchSeconds: 7, Candidates: []Candidate{
			{Title: "Solitude", URL: "master", Channel: "Nova - Topic", Duration: 200, DownloadSeconds: 30, Correct: true},
		}}},
	}

	out := Run(context.Background(), kase)

	if out.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", out.Attempts)
	}
	if out.SimulatedSeconds != 37 {
		t.Fatalf("simulated seconds = %.1f, want 37 (7s search + 30s download)", out.SimulatedSeconds)
	}
}

func TestRegressions_FlagsSimulatedSecondsAndAttemptsAboveBaseline(t *testing.T) {
	r := Report{Total: 1, Passed: 1, MedianSimulatedSeconds: 110, MeanAttempts: 2.2}
	base := Baseline{Accuracy: 1.0, MedianSeconds: 100, MeanAttempts: 2.0}

	got := r.Regressions(base)
	if len(got) != 2 {
		t.Fatalf("expected a median-seconds and a mean-attempts regression, got %v", got)
	}
}

func TestRegressions_SilentWithinTheFivePercentMargin(t *testing.T) {
	r := Report{Total: 1, Passed: 1, MedianSimulatedSeconds: 104, MeanAttempts: 2.05}
	base := Baseline{Accuracy: 1.0, MedianSeconds: 100, MeanAttempts: 2.0}

	if got := r.Regressions(base); len(got) != 0 {
		t.Fatalf("expected no regression within the 5%% margin, got %v", got)
	}
}

func TestValidateCases_RejectsEveryBidiControlInACandidateTitle(t *testing.T) {
	bidi := map[string]rune{
		"arabic letter mark":         0x061C,
		"left-to-right mark":         0x200E,
		"right-to-left mark":         0x200F,
		"left-to-right embedding":    0x202A,
		"right-to-left embedding":    0x202B,
		"pop directional formatting": 0x202C,
		"left-to-right override":     0x202D,
		"right-to-left override":     0x202E,
		"left-to-right isolate":      0x2066,
		"right-to-left isolate":      0x2067,
		"first strong isolate":       0x2068,
		"pop directional isolate":    0x2069,
	}
	for name, r := range bidi {
		t.Run(name, func(t *testing.T) {
			kase := Case{ID: "bidi-case", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Candidates: []Candidate{{URL: "u", Title: "Halo " + string(r) + "oidoV"}}}
			if err := validateCases([]Case{kase}); err == nil {
				t.Errorf("validateCases accepted candidate title %+q, want rejection", kase.Candidates[0].Title)
			}
		})
	}
}

func TestValidateCases_RejectsWhitespaceAndOtherControlsInTrackAndCandidateText(t *testing.T) {
	controls := map[string]rune{
		"nul":             0x00,
		"tab":             0x09,
		"newline":         0x0A,
		"carriage return": 0x0D,
		"unit separator":  0x1F,
		"delete":          0x7F,
		"first c1":        0x80,
		"c1 csi":          0x9B,
		"last c1":         0x9F,
	}
	for name, r := range controls {
		t.Run(name+" in track title", func(t *testing.T) {
			kase := Case{ID: "ctl-case", Class: "F1", Track: Track{Title: "Halo" + string(r), Artist: "a"}, Candidates: []Candidate{{URL: "u"}}}
			if err := validateCases([]Case{kase}); err == nil {
				t.Errorf("validateCases accepted track title %+q, want rejection", kase.Track.Title)
			}
		})
		t.Run(name+" in candidate channel", func(t *testing.T) {
			kase := Case{ID: "ctl-case", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Candidates: []Candidate{{URL: "u", Channel: string(r) + "Topic"}}}
			if err := validateCases([]Case{kase}); err == nil {
				t.Errorf("validateCases accepted candidate channel %+q, want rejection", kase.Candidates[0].Channel)
			}
		})
	}
}

func TestValidateCases_AcceptsEmojiSequencesCombiningMarksAndFullwidthText(t *testing.T) {
	texts := map[string][]rune{
		"zwj family emoji":       {'F', 0x1F468, 0x200D, 0x1F469, 0x200D, 0x1F467},
		"variation selector":     {'L', 0x2764, 0xFE0F},
		"keycap sequence":        {'1', 0xFE0F, 0x20E3},
		"skin tone modifier":     {'W', 0x1F44B, 0x1F3FD},
		"combining acute":        {'e', 0x0301},
		"stacked combining":      {'a', 0x0300, 0x0301, 0x0302},
		"fullwidth latin":        {0xFF22, 0xFF25, 0xFF39, 0xFF2F, 0xFF2E, 0xFF23, 0xFF25},
		"hangul and devanagari":  {0xBE44, 0xC5D9, ' ', 0x0939, 0x0947},
		"no-break and em spaces": {'H', 0x00A0, 'L', 0x2003, 'M'},
	}
	for name, runes := range texts {
		t.Run(name, func(t *testing.T) {
			text := string(runes)
			kase := Case{
				ID: "unicode-case", Class: "OK",
				Track:      Track{Title: text, Artist: text, Album: text},
				Candidates: []Candidate{{URL: "u", Title: text, Channel: text}},
			}
			if err := validateCases([]Case{kase}); err != nil {
				t.Errorf("validateCases rejected ordinary text %+q: %v", text, err)
			}
		})
	}
}

func TestLoadDir_RejectsAGoldenWhoseJSONEscapesAControlIntoATrackOrCandidateField(t *testing.T) {
	goldens := map[string]string{
		"escape in candidate title":            `{"cases":[{"id":"json-esc","class":"F1","track":{"title":"t","artist":"a"},"candidates":[{"url":"u","title":"Halo \\u001b[2J"}]}]}`,
		"override in source candidate channel": `{"cases":[{"id":"json-esc","class":"F1","track":{"title":"t","artist":"a"},"sources":[{"name":"s","candidates":[{"url":"u","channel":"\\u202eTopic"}]}]}]}`,
		"c1 in track album":                    `{"cases":[{"id":"json-esc","class":"F1","track":{"title":"t","artist":"a","album":"b\\u009b31m"},"candidates":[{"url":"u"}]}]}`,
	}
	for name, body := range goldens {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "hostile.json"), []byte(strings.ReplaceAll(body, `\\`, `\`)), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadDir(dir)
			if err == nil {
				t.Fatalf("LoadDir accepted golden %s, want rejection", body)
			}
			if !strings.Contains(err.Error(), `"json-esc"`) {
				t.Errorf("LoadDir error %q should name case %q", err, "json-esc")
			}
		})
	}
}

func TestValidateCases_NamesTheCaseAndFieldForArtistAlbumAndSourceCandidates(t *testing.T) {
	tests := []struct {
		name      string
		kase      Case
		wantField string
	}{
		{"track artist", Case{ID: "artist-case", Class: "F1", Track: Track{Title: "t", Artist: "a\x1b"}, Candidates: []Candidate{{URL: "u"}}}, "artist"},
		{"track album", Case{ID: "album-case", Class: "F1", Track: Track{Title: "t", Artist: "a", Album: string(rune(0x2067)) + "b"}, Candidates: []Candidate{{URL: "u"}}}, "album"},
		{"source candidate channel", Case{ID: "source-case", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Sources: []Source{{Name: "s", Candidates: []Candidate{{URL: "u", Channel: "c" + string(rune(0x202E))}}}}}, "channel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCases([]Case{tt.kase})
			if err == nil {
				t.Fatal("expected validation to reject this suite")
			}
			if msg := err.Error(); !strings.Contains(msg, `"`+tt.kase.ID+`"`) || !strings.Contains(msg, tt.wantField) {
				t.Errorf("error %q should name case %q and field %q", msg, tt.kase.ID, tt.wantField)
			}
		})
	}
}

func expectationCase(provenance, failureCode string, candidate Candidate) Case {
	return Case{
		ID: "t", Class: "F0", ExpectProvenance: provenance, ExpectFailureCode: failureCode,
		Track:      Track{Title: "Night Drive", Artist: "Halcyon Ferry", Duration: 204},
		Candidates: []Candidate{candidate},
	}
}

func TestRun_JudgesTheExpectedProvenanceAndFailureCode(t *testing.T) {
	stored := Candidate{Title: "Night Drive", URL: "u", Channel: "Halcyon Ferry - Topic", Duration: 205, Correct: true}
	rejected := Candidate{Title: "Cooking Tutorial Episode 47", URL: "cook", Channel: "Cooking", Duration: 204}
	probe := Run(context.Background(), expectationCase("", "", stored))
	failProbe := Run(context.Background(), expectationCase("", "", rejected))
	if probe.Failed || probe.Provenance == "" || !failProbe.Failed || failProbe.FailureCode == "" {
		t.Fatalf("probe runs did not store/fail as assumed: %+v %+v", probe, failProbe)
	}
	wrongProvenance := "verified"
	if probe.Provenance == wrongProvenance {
		wrongProvenance = "best_effort"
	}
	tests := []struct {
		name string
		kase Case
		pass bool
	}{
		{"provenance mismatch", expectationCase(wrongProvenance, "", stored), false},
		{"provenance match", expectationCase(probe.Provenance, "", stored), true},
		{"failure code mismatch", expectationCase("", "no_confident_match_x", rejected), false},
		{"failure code match", expectationCase("", failProbe.FailureCode, rejected), true},
		{"failure code ignored when stored", expectationCase("", "no_confident_match_x", stored), true},
		{"provenance ignored when failed", expectationCase(wrongProvenance, "", rejected), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := Run(context.Background(), tc.kase)
			if out.Pass != tc.pass {
				t.Errorf("pass = %v (%s), want %v", out.Pass, out.Reason, tc.pass)
			}
		})
	}
}

func TestLoadEmbedded_RejectsUnknownExpectedProvenance(t *testing.T) {
	kase := expectationCase("maybe", "", Candidate{URL: "u"})
	if err := validateCases([]Case{kase}); err == nil {
		t.Fatal("expected expect_provenance \"maybe\" to be rejected")
	}
}

func TestEmbeddedMustHoldCasesCarryTheirExpectations(t *testing.T) {
	want := map[string][2]string{
		"mh4-unknown-fingerprint-topic-within-2s-stored-best-effort": {"best_effort", ""},
		"mh4-unknown-fingerprint-non-topic-20s-off-fails":            {"", "no_confident_match"},
		"mh11-radio-edit-loses-to-clean-topic-upload":                {"best_effort", ""},
		"mh11-radio-edit-only-with-wrong-length-fails":               {"", "no_confident_match"},
	}
	for id, exp := range want {
		kase := embeddedCase(t, id)
		if kase.ExpectProvenance != exp[0] || kase.ExpectFailureCode != exp[1] || kase.isPending() {
			t.Errorf("%s: got %q/%q pending=%v", id, kase.ExpectProvenance, kase.ExpectFailureCode, kase.isPending())
		}
	}
}
