package eval

import (
	"altune/go-api/internal/acquisition/adapters/catalogbridge"
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"altune/go-api/internal/acquisition/adapters/ytmusic"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
		{"resolution beside a copied authoritative duration", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a", AuthoritativeDuration: 200, Resolution: &Resolution{}}, Candidates: []Candidate{{URL: "u"}}},
		}},
		{"isrc recordings for a track with no isrc", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a", Resolution: &Resolution{ISRCRecordings: []ISRCRecording{{MBID: "m"}}}}, Candidates: []Candidate{{URL: "u"}}},
		}},
		{"recorded search hit with no title", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a", Resolution: &Resolution{Search: []SearchedRecording{{Artist: "a", MBID: "m"}}}}, Candidates: []Candidate{{URL: "u"}}},
		}},
		{"recorded search hit with no artist", []Case{
			{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a", Resolution: &Resolution{Search: []SearchedRecording{{Title: "t", MBID: "m"}}}}, Candidates: []Candidate{{URL: "u"}}},
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

func otherRecordingResults(acoustID, mbid, title string, duration float64) []AcoustIDResult {
	return []AcoustIDResult{{ID: acoustID, Score: 1, Recordings: []LinkedRecording{{MBID: mbid, Title: title, Artists: []string{"Bran Van 3000"}, Duration: duration}}}}
}

func TestRun_ResolvedSearchMBIDRejectsAnUploadTheFingerprintLinksToAnotherSong(t *testing.T) {
	kase := remixMBIDCase(&Resolution{Search: []SearchedRecording{{Title: "Drinking in L.A.", Artist: "Bran Van 3000", MBID: "remix", Duration: 236}}}, "")
	kase.Candidates[0].AcoustIDResults = otherRecordingResults("album-ac", "album", "Different Song", 237)
	kase.Candidates[0].Title = "Drinking in L.A. (Radio Edit)"

	out := Run(context.Background(), kase)

	if !out.Failed {
		t.Fatalf("expected the fingerprint's other-song result and the radio-edit title together to reject the album upload, stored %q", out.Stored)
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
	kase.Candidates[0].AcoustIDResults = otherRecordingResults("album-ac", "album", "Different Song", 237)
	kase.Candidates[0].Title = "Drinking in L.A. (Radio Edit)"

	out := Run(context.Background(), kase)

	if !out.Failed {
		t.Fatalf("expected the fingerprint's other-song result and the radio-edit title together to reject the album upload, stored %q", out.Stored)
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
				Search: []SearchedRecording{{Title: "Drinking in L.A.", Artist: "Bran Van 3000", MBID: "remix", ISRC: "CAA509814003", Duration: 236}},
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

func TestRun_FallbackTimeAddsToTheFirstTier(t *testing.T) {
	kase := Case{
		ID: "t", Class: "OK",
		Track: Track{Title: "Solitude", Artist: "Nova", Duration: 200},
		Sources: []Source{
			{Name: "ytmsearch", SearchSeconds: 3, Candidates: []Candidate{
				{Title: "Solitude", URL: "art", Duration: 200, Resolved: true, Kind: "art_track", Artists: []string{"Nova"}, DownloadFails: true, DownloadSeconds: 5},
			}},
			{Name: "ytdlp", SearchSeconds: 4, Candidates: []Candidate{
				{Title: "Solitude", URL: "master", Channel: "Nova - Topic", Duration: 200, DownloadSeconds: 7, Correct: true},
			}},
		},
	}

	out := Run(context.Background(), kase)

	if out.Stored != "master" {
		t.Fatalf("stored %q, want master from the fallback tier", out.Stored)
	}
	if out.SimulatedSeconds != 19 {
		t.Fatalf("simulated seconds = %.1f, want 19 (3s + 4s of search, 5s + 7s of download)", out.SimulatedSeconds)
	}
}

func TestValidateCases_RefusesTimesBeyondProdTimeouts(t *testing.T) {
	track := Track{Title: "t", Artist: "a"}
	tests := []struct {
		name string
		kase Case
	}{
		{"a candidate slower than the candidate timeout", Case{ID: "x", Class: "F1", Track: track, Candidates: []Candidate{
			{URL: "u", DownloadSeconds: service.DefaultCandidateTimeout.Seconds() + 1},
		}}},
		{"a case slower than the job timeout", Case{ID: "x", Class: "F1", Track: track, Sources: []Source{
			{Name: "ytmsearch", SearchSeconds: 300, Candidates: []Candidate{{URL: "u", DownloadSeconds: 170}}},
			{Name: "ytdlp", SearchSeconds: 300, Candidates: []Candidate{{URL: "v", DownloadSeconds: 170}}},
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateCases([]Case{tt.kase}); err == nil {
				t.Fatal("validateCases = nil, want a refusal")
			}
		})
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

func TestRunAll_OverlappedDownloadsScoreAsTheSlowestOfTheWindow(t *testing.T) {
	kase := Case{
		ID: "t", Class: "OK",
		Track: Track{Title: "Solitude", Artist: "Nova", Duration: 200},
		Sources: []Source{{Name: "ytdlp", SearchSeconds: 0.5, Candidates: []Candidate{
			{Title: "Solitude", URL: "master", Channel: "Nova - Topic", Duration: 200, DownloadSeconds: 20, Correct: true},
			{Title: "Solitude", URL: "second", Channel: "Nova Fan", Duration: 200, DownloadSeconds: 30},
		}}},
	}

	out := RunAll(context.Background(), []Case{kase})[0]

	if out.Stored != "master" {
		t.Fatalf("stored %q, want master", out.Stored)
	}
	if out.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", out.Attempts)
	}
	if out.SimulatedSeconds != 30.5 {
		t.Fatalf("simulated seconds = %.1f, want 30.5 (0.5s search + the 30s slowest of one window)", out.SimulatedSeconds)
	}
}

func TestRun_ArtTrackByTheSearchedArtistIsTrustedWithoutAFingerprint(t *testing.T) {
	cases, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	var kase Case
	for _, c := range cases {
		if c.ID == "pl-art-track-by-the-searched-artist-is-trusted-without-a-fingerprint" {
			kase = c
		}
	}

	out := Run(context.Background(), kase)

	if !out.Pass {
		t.Fatalf("expected pass, got %q (stored %q)", out.Reason, out.Stored)
	}
}

func TestRun_CarriesTheCatalogPinsIntoTheTrack(t *testing.T) {
	explicit := false
	kase := Case{
		ID:    "pins",
		Class: "RW",
		Track: Track{
			Title: "t", Artist: "a", Duration: 167, CatalogDuration: 139,
			CatalogMBID:     "11111111-2222-3333-4444-555555555555",
			CatalogSources:  []CatalogSource{{Provider: "musicbrainz", ExternalID: "abc"}},
			CatalogExplicit: &explicit,
		},
		Candidates: []Candidate{{
			Title: "t", URL: "https://music.youtube.com/watch?v=x", Duration: 139,
			Resolved: true, Kind: "art_track", Explicit: true, Album: "Album", Correct: true,
		}},
	}

	ledger, err := catalogbridge.NewSeededLedger(seedFor(kase))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	track, err := ledger.Get(context.Background(), ledger.TrackID(), ledger.UserID())
	if err != nil || track == nil {
		t.Fatalf("Get = %v, %v", track, err)
	}
	if track.CatalogMBID == nil || *track.CatalogMBID != kase.Track.CatalogMBID {
		t.Errorf("CatalogMBID = %v", track.CatalogMBID)
	}
	if len(track.CatalogSources) != 1 || track.CatalogSources[0] != (ports.CatalogSource{Provider: "musicbrainz", ExternalID: "abc"}) {
		t.Errorf("CatalogSources = %+v", track.CatalogSources)
	}
	if track.CatalogExplicit == nil || *track.CatalogExplicit {
		t.Errorf("CatalogExplicit = %v, want false", track.CatalogExplicit)
	}
	if track.CatalogDurationSeconds == nil || *track.CatalogDurationSeconds != 139 {
		t.Errorf("CatalogDurationSeconds = %v, want the catalog length 139", track.CatalogDurationSeconds)
	}

	found, err := newCasePorts(kase).sources()[0].Find(context.Background(), ports.FindRequest{})
	if err != nil || len(found) != 1 {
		t.Fatalf("Find = %v, %v", found, err)
	}
	if !found[0].Explicit || found[0].Album != "Album" {
		t.Errorf("candidate = %+v, want explicit with album", found[0])
	}

	if out := Run(context.Background(), kase); !out.Pass {
		t.Errorf("Run did not pass: %+v", out)
	}
}

func TestRun_TheCatalogPinAloneVerifiesTheStoredUpload(t *testing.T) {
	kase := embeddedCase(t, "rw-everybody-wants-pin-verifies-the-stored-upload")

	out := Run(context.Background(), kase)

	if !out.Pass {
		t.Fatalf("expected pass, got %q (err %q)", out.Reason, out.Err)
	}
	if want := "https://www.youtube.com/watch?v=SFU1GeGFpzY"; out.Stored != want {
		t.Errorf("stored %q, want the pinned recording upload %q", out.Stored, want)
	}
	if out.Provenance != "verified" {
		t.Errorf("provenance %q, want verified: the pin must put the stored upload through the fingerprint gate", out.Provenance)
	}
}

func TestRun_PinnedTrackWithNoRecordedAnswersStillRejectsAnotherRecording(t *testing.T) {
	kase := Case{
		ID: "t", Class: "RW",
		Track: Track{Title: "Song", Artist: "Artist", Duration: 200, CatalogMBID: "pin", Resolution: &Resolution{}},
		Candidates: []Candidate{
			{Title: "Song", URL: "other-version", Channel: "Artist - Topic", Duration: 200, ViewCount: 9000000, AcoustID: "other-ac", RecordingMBIDs: []string{"other"}, AcoustIDResults: []AcoustIDResult{{ID: "other-ac", Score: 1, Recordings: []LinkedRecording{{MBID: "other", Title: "Another Song", Artists: []string{"Artist"}, Duration: 200}}}}},
			{Title: "Song", URL: "pinned", Channel: "Artist - Topic", Duration: 200, ViewCount: 10, AcoustID: "pin-ac", RecordingMBIDs: []string{"pin"}, Correct: true},
		},
	}

	out := Run(context.Background(), kase)

	if !out.Pass || out.Stored != "pinned" {
		t.Errorf("pass %v stored %q (%s), want the pinned recording's upload", out.Pass, out.Stored, out.Reason)
	}
}

func TestValidateCases_RejectsAPinnedRecordingTheCatalogDoesNotPin(t *testing.T) {
	pinned := &PinnedRecording{MBID: "pin"}
	tests := []struct {
		name        string
		catalogMBID string
	}{
		{"no catalog pin", ""},
		{"a different catalog pin", "other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cases := []Case{{
				ID: "x", Class: "RW",
				Track:      Track{Title: "t", Artist: "a", CatalogMBID: tt.catalogMBID, Resolution: &Resolution{Pinned: pinned}},
				Candidates: []Candidate{{URL: "u"}},
			}}

			if err := validateCases(cases); err == nil {
				t.Error("validateCases accepted a recorded pinned recording the catalog does not pin")
			}
		})
	}
}

func TestRun_AYTMusicAdapterSourceEmitsTheCatalogPinnedVideoFirst(t *testing.T) {
	pinned := "https://music.youtube.com/watch?v=OkjmSTJOAL4"
	kase := Case{
		ID: "pin", Class: "RW",
		Track: Track{
			Title: "Mubu", Artist: "LUCKI", Duration: 156,
			CatalogSources: []CatalogSource{{Provider: "youtube", ExternalID: "OkjmSTJOAL4"}},
		},
		Sources: []Source{{Name: "ytmusic", Adapter: "ytmusic", Candidates: []Candidate{
			{Title: "Mubu", URL: pinned, Artists: []string{"LUCKI"}, ActualDuration: 156, Correct: true},
		}}},
	}

	out := Run(context.Background(), kase)

	if !out.Pass || out.Stored != pinned {
		t.Fatalf("stored %q (%s), want the catalog-pinned video", out.Stored, out.Reason)
	}

	kase.Track.CatalogSources = nil
	if out := Run(context.Background(), kase); out.Pass {
		t.Errorf("without a catalog pin the adapter has no video to emit; stored %q", out.Stored)
	}
}

func TestValidateCases_RejectsAnUnknownSourceAdapter(t *testing.T) {
	kase := Case{
		ID: "a", Class: "RW", Track: Track{Title: "t", Artist: "a"},
		Sources: []Source{{Name: "x", Adapter: "nope", Candidates: []Candidate{{URL: "u"}}}},
	}

	if err := validateCases([]Case{kase}); err == nil {
		t.Error("expected an unknown adapter to be rejected")
	}
}

func TestRun_ACatalogPinAdapterSourceEmitsThePickedSoundCloudUpload(t *testing.T) {
	picked := "https://soundcloud.com/curre-2/chicago-finale-mp3"
	kase := Case{
		ID: "pin", Class: "RW",
		Track: Track{
			Title: "Street Player 2021 - Radio Edit", Artist: "D.J.F.",
			CatalogSources: []CatalogSource{{Provider: "soundcloud", ExternalID: "1506924427", URL: picked}},
		},
		Sources: []Source{{Name: "catalogpin", Adapter: "catalogpin", Candidates: []Candidate{
			{Title: "CURRE, D.J.F - Street Player 2021 (Edit)", URL: picked, Duration: 192.124, Correct: true},
		}}},
	}

	out := Run(context.Background(), kase)

	if !out.Pass || out.Stored != picked {
		t.Fatalf("stored %q (%s), want the picked upload", out.Stored, out.Reason)
	}

	kase.Track.CatalogSources = nil
	if out := Run(context.Background(), kase); out.Pass {
		t.Errorf("without a catalog pin the adapter emits nothing; stored %q", out.Stored)
	}
}

func TestIdentifyFake_AlwaysCarriesResultsLikeChromaprint(t *testing.T) {
	cases, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	for _, kase := range cases {
		p := newCasePorts(kase)
		for i, cand := range kase.allCandidates() {
			path := filepath.Join("fake", kase.ID, string(rune('a'+i)))
			p.byPath[path] = cand
			match, err := p.Identify(context.Background(), path, 0)
			if err != nil {
				t.Fatalf("%s %s: Identify: %v", kase.ID, cand.URL, err)
			}
			if match.Known() && len(match.Results) == 0 {
				t.Errorf("%s %s: known match %+v carries no Results, chromaprint always fills them", kase.ID, cand.URL, match)
			}
		}
	}
}

func TestRun_GoldensAssertTheSavedTrack(t *testing.T) {
	for _, id := range []string{
		"rw-drinking-in-la-remix-mbid-anchored-to-isrc",
		"pl-catalog-length-wins-over-a-doubted-search-pick",
		"reacquire-never-returns-the-upload-the-user-rejected",
	} {
		kase := embeddedCase(t, id)
		if kase.ExpectSaved == nil {
			t.Fatalf("%s: the golden asserts nothing about the saved track", id)
		}

		out := Run(context.Background(), kase)

		if !out.Pass {
			t.Errorf("%s: expected pass, got %q (err %q)", id, out.Reason, out.Err)
		}
	}
}

func TestRun_SavesTheISRCAndTheMeasuredLengthThroughTheLedger(t *testing.T) {
	out := Run(context.Background(), embeddedCase(t, "rw-drinking-in-la-remix-mbid-anchored-to-isrc"))

	want := SavedTrack{ISRC: "CAA509814003", ISRCSource: "catalog", Provenance: "best_effort", DurationSeconds: 237}
	if out.Saved.ISRC != want.ISRC || out.Saved.ISRCSource != want.ISRCSource ||
		out.Saved.Provenance != want.Provenance || out.Saved.DurationSeconds != want.DurationSeconds {
		t.Errorf("saved %+v, want %+v", out.Saved, want)
	}
}

func TestRun_AdoptsTheISRCTheFingerprintProves(t *testing.T) {
	const mbid = "aaaaaaaa-0000-4000-8000-000000000001"
	kase := Case{
		ID: "adopt", Class: "RW",
		Track: Track{
			Title: "Song", Artist: "Artist", Duration: 200,
			Resolution: &Resolution{Search: []SearchedRecording{{Title: "Song", Artist: "Artist", MBID: mbid, ISRC: "CAA509814003", Duration: 200}}},
		},
		Candidates: []Candidate{{
			Title: "Song", URL: "u", Channel: "Artist - Topic", Duration: 200, Correct: true,
			AcoustID: "ac", RecordingMBIDs: []string{mbid},
		}},
	}

	out := Run(context.Background(), kase)

	if out.Saved.ISRC != "CAA509814003" || out.Saved.ISRCSource != "fingerprint" {
		t.Errorf("saved %+v, want the fingerprint-proven ISRC written back", out.Saved)
	}
}

func TestRun_TheRecordedSearchKeepsEachHitsOwnTitle(t *testing.T) {
	const live, studio = "aaaaaaaa-0000-4000-8000-00000000000a", "aaaaaaaa-0000-4000-8000-00000000000b"
	kase := Case{
		ID: "live-first", Class: "RW",
		Track: Track{
			Title: "Song", Artist: "Artist", Duration: 200,
			Resolution: &Resolution{Search: []SearchedRecording{
				{Title: "Song (Live)", Artist: "Artist", MBID: live, Duration: 250},
				{Title: "Song", Artist: "Artist", MBID: studio, Duration: 200},
			}},
		},
		Candidates: []Candidate{{
			Title: "Song", URL: "u", Channel: "Artist - Topic", Duration: 200, Correct: true,
			AcoustID: "ac", RecordingMBIDs: []string{studio},
		}},
	}

	out := Run(context.Background(), kase)

	if out.Failed || out.Stored != "u" {
		t.Fatalf("stored %q failed=%v err=%q, want the upload linked to the studio recording", out.Stored, out.Failed, out.Err)
	}
}

func TestRun_JudgesTheExpectedSavedTrack(t *testing.T) {
	base := embeddedCase(t, "rw-drinking-in-la-remix-mbid-anchored-to-isrc")
	tests := []struct {
		name string
		want SavedExpectation
		fail string
	}{
		{"isrc", SavedExpectation{ISRC: "XX0000000000"}, "want saved isrc XX0000000000"},
		{"isrc source", SavedExpectation{ISRCSource: "fingerprint"}, "want saved isrc source fingerprint"},
		{"provenance", SavedExpectation{Provenance: "verified"}, "want saved provenance verified"},
		{"duration", SavedExpectation{DurationSeconds: 236}, "want saved duration 236"},
		{"rejected keys", SavedExpectation{RejectedKeys: []string{"youtube:abc"}}, "want saved rejected keys [youtube:abc]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kase := base
			kase.ExpectSaved = &tt.want

			out := Run(context.Background(), kase)

			if out.Pass || !strings.HasPrefix(out.Reason, tt.fail) {
				t.Errorf("pass=%v reason=%q, want a failure starting %q", out.Pass, out.Reason, tt.fail)
			}
		})
	}
}

func TestRun_ReplacingAFileOfUnknownSourceAimsAtTheCatalogNotTheStoredLength(t *testing.T) {
	out := Run(context.Background(), embeddedCase(t, "pl-replace-measured-length-is-not-the-target"))

	if !out.Pass {
		t.Fatalf("expected pass, got %q (err %q)", out.Reason, out.Err)
	}
	if want := "https://www.youtube.com/watch?v=fV3KcFFVZJ8"; out.Stored != want {
		t.Errorf("stored %q, want the artist's upload %q", out.Stored, want)
	}
}

func TestRun_ReplaceNeverStoresTheFileTheTrackAlreadyHolds(t *testing.T) {
	kase := embeddedCase(t, "replace-unknown-source-never-stores-the-current-file-again")

	out := Run(context.Background(), kase)

	if !out.Pass || out.Stored != "" {
		t.Errorf("pass=%v stored=%q reason=%q, want nothing stored", out.Pass, out.Stored, out.Reason)
	}
	unfingerprinted := kase
	unfingerprinted.Stored = &StoredFile{Duration: kase.Stored.Duration}
	if out := Run(context.Background(), unfingerprinted); out.Pass {
		t.Errorf("without a fingerprint of the stored file the upload wins and the case must fail; stored %q", out.Stored)
	}
}

func TestRun_ASeededRejectedSourceNeverComesBack(t *testing.T) {
	kase := embeddedCase(t, "reacquire-fails-rather-than-return-the-only-upload-the-user-rejected")

	out := Run(context.Background(), kase)

	if !out.Pass || out.Stored != "" {
		t.Errorf("pass=%v stored=%q reason=%q, want the rejected upload refused", out.Pass, out.Stored, out.Reason)
	}
}

func TestRun_TheRejectedKeyStaysOnTheSavedTrack(t *testing.T) {
	out := Run(context.Background(), embeddedCase(t, "reacquire-never-returns-the-upload-the-user-rejected"))

	if want := []string{"youtube:FjkkqbEbFOY"}; !slices.Equal(out.Saved.RejectedKeys, want) {
		t.Errorf("saved rejected keys %v, want %v", out.Saved.RejectedKeys, want)
	}
}

func TestValidateCases_RejectsAStoredFileWithNoDuration(t *testing.T) {
	cases := []Case{{
		ID: "x", Class: "OK", Stored: &StoredFile{},
		Track:      Track{Title: "t", Artist: "a"},
		Candidates: []Candidate{{URL: "u"}},
	}}

	if err := validateCases(cases); err == nil || !strings.Contains(err.Error(), "stores a file with no duration") {
		t.Fatalf("validateCases = %v, want a stored-file duration error", err)
	}
}

func TestValidateCases_RejectsAnUnknownExpectedSavedProvenance(t *testing.T) {
	cases := []Case{{
		ID: "x", Class: "OK", ExpectSaved: &SavedExpectation{Provenance: "made_up"},
		Track:      Track{Title: "t", Artist: "a"},
		Candidates: []Candidate{{URL: "u"}},
	}}

	if err := validateCases(cases); err == nil || !strings.Contains(err.Error(), "unknown saved provenance") {
		t.Fatalf("validateCases = %v, want an unknown saved provenance error", err)
	}
}

func TestLoad_RefusesShapesProdNeverEmits(t *testing.T) {
	goldens := map[string]struct{ candidate, want string }{
		"resolved with a real channel and no kind": {`{"title":"t","url":"u","channel":"YouTube Music catalog","resolved":true}`, `has resolved=true but kind "" reads as resolved=false`},
		"art_track that is not resolved":           {`{"title":"t","url":"u","kind":"art_track"}`, `has resolved=false but kind "art_track" reads as resolved=true`},
		"upload that is resolved":                  {`{"title":"t","url":"u","kind":"upload","resolved":true}`, `has resolved=true but kind "upload" reads as resolved=false`},
	}
	for name, g := range goldens {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			body := `{"cases":[{"id":"shape","class":"F1","track":{"title":"t","artist":"a"},"candidates":[` + g.candidate + `]}]}`
			if err := os.WriteFile(filepath.Join(dir, "shape.json"), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}

			_, err := LoadDir(dir)

			if err == nil || !strings.Contains(err.Error(), g.want) || !strings.Contains(err.Error(), `"shape"`) {
				t.Fatalf("LoadDir = %v, want a refusal naming case %q and %q", err, "shape", g.want)
			}
		})
	}
}

func TestRun_FlatCandidatesRunInProdTiersByKind(t *testing.T) {
	kase := Case{
		ID: "t", Class: "OK",
		Track: Track{Title: "Solitude", Artist: "Nova", Duration: 200, AuthoritativeDuration: 200},
		Candidates: []Candidate{
			{Title: "Solitude", URL: "upload", Channel: "Nova - Topic", Duration: 200, ViewCount: 9000000},
			{Title: "Solitude", URL: "art", Duration: 200, Resolved: true, Kind: "art_track", Artists: []string{"Nova"}, Correct: true},
		},
	}

	out := Run(context.Background(), kase)

	if !out.Pass || out.Stored != "art" {
		t.Fatalf("stored %q (%s), want the art track", out.Stored, out.Reason)
	}
	if out.Attempts != 1 {
		t.Errorf("attempts = %d, want 1: prod never searches uploads once a resolved candidate exists", out.Attempts)
	}
}

type fixedSongs []ytmsearch.Song

func (f fixedSongs) SearchSongs(context.Context, string) ([]ytmsearch.Song, error) { return f, nil }

type fixedWatch ytmsearch.Watch

func (f fixedWatch) Watch(context.Context, string) (ytmsearch.Watch, error) {
	return ytmsearch.Watch(f), nil
}

func writeGolden(t *testing.T, candidate string) string {
	t.Helper()
	dir := t.TempDir()
	body := `{"cases":[{"id":"shape","class":"OK","track":{"title":"Halo","artist":"Nova"},"candidates":[` + candidate + `]}]}`
	if err := os.WriteFile(filepath.Join(dir, "shape.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func loadedCandidate(t *testing.T, candidate string) Candidate {
	t.Helper()
	cases, err := LoadDir(writeGolden(t, candidate))
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(cases) != 1 || len(cases[0].Candidates) != 1 {
		t.Fatalf("loaded %d cases, want one case with one candidate", len(cases))
	}
	return cases[0].Candidates[0]
}

func TestRun_ObservedCandidatesGoThroughTheAdapters(t *testing.T) {
	ctx := context.Background()
	req := ports.FindRequest{Title: "Halo", Artist: "Nova"}
	pinReq := req
	pinReq.CatalogSources = []ports.CatalogSource{{Provider: string(ports.ProviderYouTube), ExternalID: "abcdefghijk"}}

	t.Run("ytmsearch song", func(t *testing.T) {
		song := ytmsearch.Song{VideoID: "abcdefghijk", Title: "Halo", Artists: []string{"Nova"}, Album: "Glow", DurationSeconds: 201, Type: ytmsearch.VideoTypeATV, Explicit: true}
		want, err := ytmsearch.NewSource(fixedSongs{song}, nil).Find(ctx, req)
		if err != nil || len(want) != 1 {
			t.Fatalf("ytmsearch Find = %v, %v", want, err)
		}

		got := loadedCandidate(t, `{"observed":{"ytmsearch_song":{"VideoID":"abcdefghijk","Title":"Halo","Artists":["Nova"],"Album":"Glow","DurationSeconds":201,"Type":"MUSIC_VIDEO_TYPE_ATV","Explicit":true}}}`).audioCandidate()

		if !reflect.DeepEqual(got, want[0]) {
			t.Errorf("golden candidate = %+v, want what ytmsearch Find emits: %+v", got, want[0])
		}
	})

	t.Run("ytmusic watch", func(t *testing.T) {
		watch := ytmsearch.Watch{VideoID: "abcdefghijk", Title: "Halo", Artists: []string{"Nova"}, Type: ytmsearch.VideoTypeOMV}
		want, err := ytmusic.NewSource(nil, fixedWatch(watch)).Find(ctx, pinReq)
		if err != nil || len(want) != 1 {
			t.Fatalf("ytmusic Find = %v, %v", want, err)
		}

		got := loadedCandidate(t, `{"observed":{"ytmusic_watch":{"VideoID":"abcdefghijk","Title":"Halo","Artists":["Nova"],"Type":"MUSIC_VIDEO_TYPE_OMV","Pinned":true}}}`).audioCandidate()

		if !reflect.DeepEqual(got, want[0]) {
			t.Errorf("golden candidate = %+v, want what ytmusic Find emits: %+v", got, want[0])
		}
	})

	t.Run("ytdlp entry", func(t *testing.T) {
		line := `{"title":"Halo","duration":201.5,"webpage_url":"https://soundcloud.com/nova/halo","view_count":7}`
		var entry ytdlp.Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}

		got := loadedCandidate(t, `{"observed":{"ytdlp_entry":`+line+`}}`).audioCandidate()

		if want := ytdlp.CandidateFromEntry(entry); !reflect.DeepEqual(got, want) {
			t.Errorf("golden candidate = %+v, want what ytdlp emits: %+v", got, want)
		}
		if got.Kind != ports.KindUpload || got.IsResolved() {
			t.Errorf("golden candidate kind %q resolved %v, want an unresolved upload", got.Kind, got.IsResolved())
		}
	})
}

func TestLoad_RefusesAnObservedCandidateThatAlsoSetsWhatTheAdapterEmits(t *testing.T) {
	for name, candidate := range map[string]string{
		"a hand-set channel":       `{"channel":"ezcodylee","observed":{"ytdlp_entry":{"webpage_url":"https://soundcloud.com/a/b"}}}`,
		"two observed forms":       `{"observed":{"ytdlp_entry":{"webpage_url":"https://soundcloud.com/a/b"},"ytmsearch_song":{"VideoID":"abcdefghijk"}}}`,
		"an observed with no form": `{"observed":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadDir(writeGolden(t, candidate)); err == nil || !strings.Contains(err.Error(), `"shape"`) {
				t.Fatalf("LoadDir = %v, want a refusal naming case %q", err, "shape")
			}
		})
	}
}

func TestGoldenWatches_CarryTheWatchTypeProdObserved(t *testing.T) {
	url := "https://music.youtube.com/watch?v=abcdefghijk"
	observed := ytmsearch.Watch{VideoID: "abcdefghijk", Title: "Halo", Type: ytmsearch.VideoTypeOMV}
	tests := []struct {
		name      string
		candidate Candidate
		want      ytmsearch.VideoType
	}{
		{"an art track is an ATV", Candidate{URL: url, Kind: "art_track", Resolved: true}, ytmsearch.VideoTypeATV},
		{"an upload is a UGC", Candidate{URL: url, Kind: "upload"}, ytmsearch.VideoTypeUGC},
		{"an observed watch is returned as observed", Candidate{URL: url, Observed: &Observed{Watch: &ObservedWatch{Watch: observed}}}, ytmsearch.VideoTypeOMV},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := goldenWatches{candidates: []Candidate{tt.candidate}}.Watch(context.Background(), "abcdefghijk")

			if err != nil || got.Type != tt.want {
				t.Errorf("Watch = %+v, %v; want type %q", got, err, tt.want)
			}
		})
	}
}

func TestEmbedded_TheCatalogLengthGoldenIsWhatProdsAdaptersEmit(t *testing.T) {
	kase := embeddedCase(t, "pl-catalog-length-wins-over-a-doubted-search-pick")

	var artTracks, uploads int
	for _, cand := range kase.allCandidates() {
		switch ports.CandidateKind(cand.Kind) {
		case ports.KindArtTrack:
			artTracks++
			if !cand.Resolved || cand.Channel != "" || len(cand.Categories) != 0 || cand.Duration != 140 {
				t.Errorf("art track %+v is not what ytmsearch emits for the 140 s hit", cand)
			}
		case ports.KindUpload:
			uploads++
			if cand.Resolved || cand.Channel != "" {
				t.Errorf("upload %+v is not what ytdlp emits for a SoundCloud result", cand)
			}
		default:
			t.Errorf("candidate %q has kind %q: prod's ytmusic emits nothing for this resolution", cand.URL, cand.Kind)
		}
	}
	if artTracks != 1 || uploads < 2 {
		t.Errorf("got %d art tracks and %d uploads, want the one ytmsearch hit and the scsearch results", artTracks, uploads)
	}
	if out := Run(context.Background(), kase); !out.Pass {
		t.Errorf("expected pass, got %q (err %q)", out.Reason, out.Err)
	}
}

var liveSearchGoldens = []string{"pipeline.json", "current_file.json", "verification.json", "art_track_length.json"}

const handBuiltNoteMarker = "hand-built because"

func TestEmbedded_LiveSearchCandidatesAreObserved(t *testing.T) {
	for _, name := range liveSearchGoldens {
		raw, err := goldenFS.ReadFile("goldens/" + name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		cases, err := decodeSuite(name, raw)
		if err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		for _, kase := range cases {
			if strings.Contains(kase.Note, handBuiltNoteMarker) {
				continue
			}
			for _, cand := range kase.allCandidates() {
				if cand.Observed == nil && strings.HasPrefix(cand.URL, "http") {
					t.Errorf("%s: case %q: live-search candidate %q is hand-built; record the adapter's raw input under observed, or say %q in the case note", name, kase.ID, cand.URL, handBuiltNoteMarker)
				}
			}
		}
	}
}

func TestValidateCases_RefusesAResolvedFlagTheKindDenies(t *testing.T) {
	for name, cand := range map[string]Candidate{
		"resolved upload":       {URL: "u", Resolved: true, Kind: "upload"},
		"resolved with no kind": {URL: "u", Resolved: true},
		"unresolved pin":        {URL: "u", Kind: "pinned_video"},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateCases([]Case{{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Candidates: []Candidate{cand}}})
			if err == nil {
				t.Fatal("validateCases accepted a resolved flag the kind denies")
			}
		})
	}
}

func TestRun_AnUnplayableCandidateIsNeverStored(t *testing.T) {
	kase := embeddedCase(t, "unplayable-soundcloud-previews-are-never-stored-the-full-upload-is")
	const fullUpload = "https://soundcloud.com/koolandthegang/get-down-on-it"

	t.Run("the full upload is stored beside the previews", func(t *testing.T) {
		out := Run(context.Background(), kase)

		if !out.Pass || out.Stored != fullUpload {
			t.Fatalf("Run stored %q (pass=%v: %s), want the full upload %q", out.Stored, out.Pass, out.Reason, fullUpload)
		}
		if out.TopRanked != fullUpload || out.Attempts != 1 {
			t.Errorf("ranked %q and attempted %d candidates, want only the full upload ranked and attempted", out.TopRanked, out.Attempts)
		}
	})

	t.Run("a preview listed at the full length is rejected before any download", func(t *testing.T) {
		var previews []Candidate
		for _, c := range kase.allCandidates() {
			if c.Unplayable == ports.UnplayablePreview {
				c.Duration, c.ActualDuration, c.Correct = kase.Track.CatalogDuration, 30, false
				previews = append(previews, c)
			}
		}
		if len(previews) == 0 {
			t.Fatal("the golden holds no candidate marked a preview")
		}
		kase.Sources = []Source{{Name: "ytdlp", SearchSeconds: 3, Candidates: previews}}

		out := Run(context.Background(), kase)

		if out.Stored != "" || out.Attempts != 0 {
			t.Errorf("stored %q after %d download attempts, want no download of a preview", out.Stored, out.Attempts)
		}
	})
}

func TestLoad_RefusesAnUnplayableReasonProdNeverSets(t *testing.T) {
	_, err := LoadDir(writeGolden(t, `{"url":"https://soundcloud.com/a/b","unplayable":"paywall"}`))
	if err == nil || !strings.Contains(err.Error(), `"paywall"`) {
		t.Fatalf("LoadDir = %v, want a refusal naming the unplayable value", err)
	}
}

func TestRun_AReplaceExcludesTheStoredSource(t *testing.T) {
	for name, storedURL := range map[string]string{
		"a stored music.youtube.com url excludes its youtube.com twin": "https://music.youtube.com/watch?v=7wBgcalM4c4",
		"a stored youtube.com url excludes its music.youtube.com twin": "https://www.youtube.com/watch?v=7wBgcalM4c4",
	} {
		t.Run(name, func(t *testing.T) {
			kase := embeddedCase(t, "replace-known-source-excludes-the-stored-source-by-its-url")
			kase.Track.AudioSourceURL = storedURL

			out := Run(context.Background(), kase)

			if !out.Pass || out.Stored != "" {
				t.Errorf("pass=%v stored=%q reason=%q, want nothing stored: the stored source is excluded by its url", out.Pass, out.Stored, out.Reason)
			}
		})
	}
}

func TestValidateCases_AcceptsASourceURLForATrackWithNoStoredFile(t *testing.T) {
	cases := []Case{{
		ID: "x", Class: "OK",
		Track:      Track{Title: "t", Artist: "a", AudioSourceURL: "https://www.youtube.com/watch?v=7wBgcalM4c4"},
		Candidates: []Candidate{{URL: "u"}},
	}}

	if err := validateCases(cases); err != nil {
		t.Fatalf("validateCases = %v, want nil", err)
	}
}

func TestRun_RecordedObservationsGoThroughTheRealAdapters(t *testing.T) {
	const explicitURL = "https://music.youtube.com/watch?v=bcdefghijkl"
	const cleanURL = "https://music.youtube.com/watch?v=abcdefghijk"
	recorded := func(pin []CatalogSource) Case {
		return Case{
			ID: "x", Class: "OK",
			Track: Track{Title: "Halo", Artist: "Nova", CatalogDuration: 200, CatalogSources: pin},
			Sources: []Source{
				{Name: "ytmsearch", ObservedSongs: []ObservedSong{
					{VideoID: "zyxwvutsrqp", Title: "Halo (Live)", Artists: []string{"Nova"}, DurationSeconds: 200, Type: ytmsearch.VideoTypeOMV},
					{VideoID: "abcdefghijk", Title: "Halo", Artists: []string{"Nova"}, DurationSeconds: 200, Type: ytmsearch.VideoTypeATV},
					{VideoID: "bcdefghijkl", Title: "Halo", Artists: []string{"Nova"}, DurationSeconds: 200, Type: ytmsearch.VideoTypeATV, Explicit: true},
				}, Candidates: []Candidate{
					{URL: cleanURL, ActualDuration: 200},
					{URL: explicitURL, ActualDuration: 200, Correct: true},
				}},
				{Name: "ytmusic", ObservedWatches: map[string]ObservedWatchRecord{
					"abcdefghijk": {Title: "Halo", Artists: []string{"Nova"}, Type: ytmsearch.VideoTypeATV},
				}},
			},
		}
	}

	t.Run("ytmsearch stores the strong hit its own tie-break picks among the recorded songs", func(t *testing.T) {
		out := Run(context.Background(), recorded(nil))

		if !out.Pass || out.Stored != explicitURL {
			t.Fatalf("Run stored %q (pass=%v: %s), want the explicit art track %q", out.Stored, out.Pass, out.Reason, explicitURL)
		}
	})

	t.Run("ytmusic yields no candidate when the request pins no video", func(t *testing.T) {
		kase := recorded(nil)
		watches := newCasePorts(kase).sources()[1]

		got, err := watches.Find(context.Background(), ports.FindRequest{Title: "Halo", Artist: "Nova"})

		if err != nil || len(got) != 0 {
			t.Errorf("Find = %v, %v, want no candidate", got, err)
		}
	})

	t.Run("ytmusic emits the recorded watch of the pinned video", func(t *testing.T) {
		kase := recorded([]CatalogSource{{Provider: string(ports.ProviderYouTube), ExternalID: "abcdefghijk"}})
		watches := newCasePorts(kase).sources()[1]

		got, err := watches.Find(context.Background(), ports.FindRequest{Title: "Halo", Artist: "Nova", CatalogSources: kase.Track.portCatalogSources()})

		if err != nil || len(got) != 1 || got[0].URL != cleanURL || got[0].Kind != ports.KindArtTrack {
			t.Errorf("Find = %+v, %v, want the pinned art track at %q", got, err, cleanURL)
		}
	})
}

func TestValidateCases_RefusesARecordedSourceThatBreaksItsAdapter(t *testing.T) {
	song := []ObservedSong{{VideoID: "abcdefghijk", Title: "Halo", Type: ytmsearch.VideoTypeATV}}
	watch := map[string]ObservedWatchRecord{"abcdefghijk": {Title: "Halo", Type: ytmsearch.VideoTypeATV}}
	facts := []Candidate{{URL: "u"}}
	tests := []struct {
		name string
		src  Source
		want string
	}{
		{"songs on a source that is not ytmsearch", Source{Name: "ytdlp", ObservedSongs: song, Candidates: facts}, "records ytmsearch songs"},
		{"watches on a source that is not ytmusic", Source{Name: "ytmsearch", ObservedWatches: watch, Candidates: facts}, "records ytmusic watches"},
		{"songs beside a named adapter", Source{Name: "ytmsearch", Adapter: "ytmusic", ObservedSongs: song, Candidates: facts}, "keep one"},
		{"a hand-set title beside the recording", Source{Name: "ytmsearch", ObservedSongs: song, Candidates: []Candidate{{URL: "u", Title: "Halo"}}}, "may carry only a url and download facts"},
		{"an observed candidate beside the songs", Source{Name: "ytmsearch", ObservedSongs: song, Candidates: []Candidate{{URL: "u", Observed: &Observed{}}}}, "may carry only a url and download facts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCases([]Case{{ID: "x", Class: "F1", Track: Track{Title: "t", Artist: "a"}, Sources: []Source{tt.src}}})

			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateCases = %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

func TestRun_ItsOnlyLoveStoresTheClassicVersionWhenTheOriginalIsNowhere(t *testing.T) {
	kase := embeddedCase(t, "rw-its-only-love-original-nowhere-takes-classic-version")

	out := Run(context.Background(), kase)

	if !out.Pass {
		t.Fatalf("expected pass, got %q (err %q, saved %+v)", out.Reason, out.Err, out.Saved)
	}
	if want := "https://music.youtube.com/watch?v=Ml2N72dMv_8"; out.Stored != want {
		t.Errorf("stored %q, want the Classic Version %q", out.Stored, want)
	}
}

func TestRun_ItsOnlyLoveWithoutTheRerecordingStoresNothing(t *testing.T) {
	kase := embeddedCase(t, "rw-its-only-love-original-nowhere-takes-classic-version")
	kase.Rerecording = nil

	out := Run(context.Background(), kase)

	if !out.Failed {
		t.Fatalf("expected a failure, stored %q", out.Stored)
	}
	if out.FailureCode != "no_confident_match" && out.FailureCode != "no_match_found" {
		t.Errorf("failure code %q, want no_confident_match or no_match_found", out.FailureCode)
	}
	if out.Stored != "" || out.Saved.CatalogMBID != "" && out.Saved.CatalogMBID != kase.Track.CatalogMBID {
		t.Errorf("stored %q, saved %+v; want nothing stored and the original label kept", out.Stored, out.Saved)
	}
}

func TestJudgeSaved_FailsOnADifferingTitleAlbumYearOrMBID(t *testing.T) {
	saved := SavedTrack{Title: "T", Album: "A", Year: 2025, CatalogMBID: "m"}
	tests := []struct {
		name string
		want SavedExpectation
	}{
		{"title", SavedExpectation{Title: "other"}},
		{"album", SavedExpectation{Album: "other"}},
		{"year", SavedExpectation{Year: 1984}},
		{"mbid", SavedExpectation{MBID: "other"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if reason, ok := judgeSaved(&tt.want, Outcome{Saved: saved}); ok {
				t.Fatalf("expected a failure for a differing %s, got pass %q", tt.name, reason)
			}
		})
	}
	if reason, ok := judgeSaved(&SavedExpectation{Title: "T", Album: "A", Year: 2025, MBID: "m"}, Outcome{Saved: saved}); !ok {
		t.Fatalf("expected a match, got %q", reason)
	}
}
