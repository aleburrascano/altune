package eval_test

import (
	"altune/go-api/internal/acquisition/internal/adapters/eval"
	"altune/go-api/internal/acquisition/internal/adapters/ytdlp"
	"altune/go-api/internal/acquisition/internal/adapters/ytmsearch"
	"altune/go-api/internal/acquisition/internal/adapters/ytmusic"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/service"
	shared "altune/go-api/internal/shared/kernel"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func validate(t *testing.T, cases []eval.Case) error {
	t.Helper()
	body, err := json.Marshal(struct {
		Cases []eval.Case `json:"cases"`
	}{cases})
	if err != nil {
		t.Fatalf("marshal cases: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cases.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = eval.LoadDir(dir)
	return err
}

func allCandidates(c eval.Case) []eval.Candidate {
	all := slices.Clone(c.Candidates)
	for _, src := range c.Sources {
		all = append(all, src.Candidates...)
	}
	return all
}

func TestLoadEmbedded_ValidatesAndSorts(t *testing.T) {
	cases, err := eval.LoadEmbedded()
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

func TestLoadDir_RejectsMalformed(t *testing.T) {
	tests := []struct {
		name  string
		cases []eval.Case
	}{
		{"no id", []eval.Case{{Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Candidates: []eval.Candidate{{URL: "u"}}}}},
		{"no class", []eval.Case{{ID: "x", Track: eval.Track{Title: "t", Artist: "a"}, Candidates: []eval.Candidate{{URL: "u"}}}}},
		{"no artist", []eval.Case{{ID: "x", Class: "F1", Track: eval.Track{Title: "t"}, Candidates: []eval.Candidate{{URL: "u"}}}}},
		{"no candidates", []eval.Case{{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}}}},
		{"candidate without url", []eval.Case{{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Candidates: []eval.Candidate{{Title: "c"}}}}},
		{"duplicate id", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Candidates: []eval.Candidate{{URL: "u"}}},
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Candidates: []eval.Candidate{{URL: "u"}}},
		}},
		{"duplicate candidate url", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Candidates: []eval.Candidate{{URL: "u"}, {URL: "u"}}},
		}},
		{"pending with a blank owning ticket", []eval.Case{
			{ID: "x", Class: "F1", Pending: "  ", Track: eval.Track{Title: "t", Artist: "a"}, Candidates: []eval.Candidate{{URL: "u"}}},
		}},
		{"resolution beside a copied mbid", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a", MBID: "m", Resolution: &eval.Resolution{}}, Candidates: []eval.Candidate{{URL: "u"}}},
		}},
		{"resolution beside a copied authoritative duration", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a", AuthoritativeDuration: 200, Resolution: &eval.Resolution{}}, Candidates: []eval.Candidate{{URL: "u"}}},
		}},
		{"isrc recordings for a track with no isrc", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a", Resolution: &eval.Resolution{ISRCRecordings: []eval.ISRCRecording{{MBID: "m"}}}}, Candidates: []eval.Candidate{{URL: "u"}}},
		}},
		{"recorded search hit with no title", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a", Resolution: &eval.Resolution{Search: []eval.SearchedRecording{{Artist: "a", MBID: "m"}}}}, Candidates: []eval.Candidate{{URL: "u"}}},
		}},
		{"recorded search hit with no artist", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a", Resolution: &eval.Resolution{Search: []eval.SearchedRecording{{Title: "t", MBID: "m"}}}}, Candidates: []eval.Candidate{{URL: "u"}}},
		}},
		{"isrc recording with no mbid", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a", ISRC: "I", Resolution: &eval.Resolution{ISRCRecordings: []eval.ISRCRecording{{Duration: 200}}}}, Candidates: []eval.Candidate{{URL: "u"}}},
		}},
		{"candidates and sources both set", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Candidates: []eval.Candidate{{URL: "u"}}, Sources: []eval.Source{{Name: "s", Candidates: []eval.Candidate{{URL: "v"}}}}},
		}},
		{"source with no name", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Sources: []eval.Source{{Candidates: []eval.Candidate{{URL: "u"}}}}},
		}},
		{"source with no candidates and no simulated failure", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Sources: []eval.Source{{Name: "s"}}},
		}},
		{"candidate with an unknown query variant", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Candidates: []eval.Candidate{{URL: "u", Query: "bogus"}}},
		}},
		{"candidate title with a terminal escape", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Candidates: []eval.Candidate{{URL: "u", Title: "Halo \x1b[31m"}}},
		}},
		{"candidate channel with a bidi override", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Candidates: []eval.Candidate{{URL: "u", Channel: "Topic \u202e"}}},
		}},
		{"track title with a bell", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t\x07", Artist: "a"}, Candidates: []eval.Candidate{{URL: "u"}}},
		}},
		{"track artist with a C1 control", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a\u009b"}, Candidates: []eval.Candidate{{URL: "u"}}},
		}},
		{"track album with a bidi isolate", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a", Album: "\u2066b"}, Candidates: []eval.Candidate{{URL: "u"}}},
		}},
		{"named-source candidate title with a delete", []eval.Case{
			{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Sources: []eval.Source{{Name: "s", Candidates: []eval.Candidate{{URL: "u", Title: "c\x7f"}}}}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validate(t, tt.cases); err == nil {
				t.Error("expected validation to reject this suite")
			}
		})
	}
}

func TestLoadDir_NamesTheCaseAndFieldHoldingAnUnsafeRune(t *testing.T) {
	tests := []struct {
		name      string
		kase      eval.Case
		wantField string
	}{
		{"candidate title escape", eval.Case{ID: "halo-esc", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Candidates: []eval.Candidate{{URL: "u", Title: "Halo \x1b[31m"}}}, `candidate "u" title`},
		{"candidate channel bidi", eval.Case{ID: "halo-bidi", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Candidates: []eval.Candidate{{URL: "u", Channel: "Topic \u202e"}}}, `candidate "u" channel`},
		{"track title bell", eval.Case{ID: "halo-bell", Class: "F1", Track: eval.Track{Title: "t\x07", Artist: "a"}, Candidates: []eval.Candidate{{URL: "u"}}}, "track title"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(t, []eval.Case{tt.kase})
			if err == nil {
				t.Fatal("expected validation to reject this suite")
			}
			msg := err.Error()
			if !strings.Contains(msg, `"`+tt.kase.ID+`"`) || !strings.Contains(msg, tt.wantField) {
				t.Errorf("error %q should name case %q and field %q", msg, tt.kase.ID, tt.wantField)
			}
			if strings.ContainsFunc(msg, func(r rune) bool { return r == '\x1b' || r == '\u202e' || r == '\x07' }) {
				t.Errorf("error %q echoes the unsafe rune raw", msg)
			}
		})
	}
}

func TestLoadDir_AcceptsOrdinaryUnicodeText(t *testing.T) {
	kase := eval.Case{
		ID: "beyonce-halo", Class: "OK",
		Track: eval.Track{Title: "Halo", Artist: "Beyoncé", Album: "I Am... Sasha Fierce"},
		Candidates: []eval.Candidate{
			{URL: "u", Title: "Beyoncé – Halo (Official Video) 🎵", Channel: "ビヨンセ · Topic"},
			{URL: "v", Title: "هالو – بيونسيه", Channel: "Beyoncé VEVO"},
		},
	}
	if err := validate(t, []eval.Case{kase}); err != nil {
		t.Fatalf("expected ordinary unicode to pass, got %v", err)
	}
}

func TestRun_StoresTheCorrectRecording(t *testing.T) {
	kase := eval.Case{
		ID: "t", Class: "OK",
		Track: eval.Track{Title: "Blinding Lights", Artist: "The Weeknd", Duration: 200},
		Candidates: []eval.Candidate{
			{Title: "Blinding Lights (Slowed)", URL: "slowed", Channel: "The Weeknd - Topic", Duration: 240},
			{Title: "Blinding Lights", URL: "master", Channel: "The Weeknd - Topic", Duration: 200, Correct: true},
		},
	}
	out := eval.Run(context.Background(), kase)
	if !out.Pass {
		t.Fatalf("expected pass, got %q (stored %q)", out.Reason, out.Stored)
	}
	if out.Stored != "master" {
		t.Errorf("stored %q, want master", out.Stored)
	}
}

func TestRun_FailsWhenWrongRecordingStored(t *testing.T) {
	kase := eval.Case{
		ID: "t", Class: "F1",
		Track:      eval.Track{Title: "Blinding Lights", Artist: "The Weeknd", Duration: 200},
		Candidates: []eval.Candidate{{Title: "Blinding Lights", URL: "cover", Channel: "Covers", Duration: 200}},
	}
	out := eval.Run(context.Background(), kase)
	if out.Pass {
		t.Fatal("expected a fail: no candidate was the right recording")
	}
	if out.Stored != "cover" {
		t.Errorf("expected the harness to record what was wrongly stored, got %q", out.Stored)
	}
}

func TestRun_PassesWhenNothingCorrectAndNothingStored(t *testing.T) {
	kase := eval.Case{
		ID: "t", Class: "F7",
		Track:      eval.Track{Title: "Save Your Tears", Artist: "The Weeknd", Duration: 215},
		Candidates: []eval.Candidate{{Title: "Cooking Tutorial Episode 47", URL: "cook", Channel: "Cooking", Duration: 215}},
	}
	out := eval.Run(context.Background(), kase)
	if !out.Pass {
		t.Fatalf("expected pass, got %q", out.Reason)
	}
	if !out.Failed {
		t.Error("expected the pipeline to reject every candidate")
	}
}

func TestRun_LeavesNoTempFiles(t *testing.T) {
	kase := eval.Case{
		ID: "t", Class: "OK",
		Track:      eval.Track{Title: "Circles", Artist: "Post Malone", Duration: 215},
		Candidates: []eval.Candidate{{Title: "Circles", URL: "u", Channel: "Post Malone - Topic", Duration: 215, Correct: true}},
	}
	out := eval.Run(context.Background(), kase)
	if !out.Pass {
		t.Fatalf("expected pass, got %q", out.Reason)
	}
}

func TestSummarize_GroupsByClassAndCollectsFailures(t *testing.T) {
	outcomes := []eval.Outcome{
		{Case: eval.Case{ID: "a", Class: "F1"}, Pass: true},
		{Case: eval.Case{ID: "b", Class: "F1"}, Pass: false, Reason: "wrong"},
		{Case: eval.Case{ID: "c", Class: "F2"}, Pass: true},
	}
	r := eval.Summarize(outcomes)

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
	r := eval.Report{Total: 2, Passed: 1, Classes: []eval.ClassResult{{Class: "F1", Total: 2, Passed: 1}}}
	base := eval.Baseline{Accuracy: 1.0, Classes: map[string]float64{"F1": 1.0}}

	got := r.Regressions(base)
	if len(got) != 2 {
		t.Fatalf("expected an overall and a per-class regression, got %v", got)
	}
}

func TestRegressions_SilentWhenAtBaseline(t *testing.T) {
	r := eval.Report{Total: 2, Passed: 2, Classes: []eval.ClassResult{{Class: "F1", Total: 2, Passed: 2}}}
	base := eval.Baseline{Accuracy: 1.0, Classes: map[string]float64{"F1": 1.0}}

	if got := r.Regressions(base); len(got) != 0 {
		t.Fatalf("expected no regressions, got %v", got)
	}
}

func TestEmbeddedSuiteMatchesCommittedBaseline(t *testing.T) {
	cases, err := eval.LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	base, err := eval.LoadBaseline("../../../../../cmd/acquisitioneval/baselines.json")
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}
	report := eval.Summarize(eval.RunAll(context.Background(), cases))

	if regressions := report.Regressions(base); len(regressions) > 0 {
		t.Errorf("embedded suite regressed against the committed baseline: %v", regressions)
	}
}

func TestLoadDir_AcceptsAResolutionWithOnlyAnISRCAnswer(t *testing.T) {
	cases := []eval.Case{{
		ID: "x", Class: "RW", Pending: "owning ticket",
		Track: eval.Track{
			Title: "t", Artist: "a", ISRC: "I",
			Resolution: &eval.Resolution{ISRCRecordings: []eval.ISRCRecording{{MBID: "m", Duration: 200}}},
		},
		Candidates: []eval.Candidate{{URL: "u"}},
	}}

	if err := validate(t, cases); err != nil {
		t.Fatalf("LoadDir rejected a well-formed resolution case: %v", err)
	}
}

func embeddedCase(t *testing.T, id string) eval.Case {
	t.Helper()
	cases, err := eval.LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	for _, c := range cases {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no embedded case %q", id)
	return eval.Case{}
}

func TestRun_DrinkingInLAResolvesTheRemixMBIDToTheISRCRecording(t *testing.T) {
	kase := embeddedCase(t, "rw-drinking-in-la-remix-mbid-anchored-to-isrc")

	out := eval.Run(context.Background(), kase)

	if !out.Pass {
		t.Fatalf("expected pass, got %q (err %q)", out.Reason, out.Err)
	}
	if want := "https://www.youtube.com/watch?v=-_5yNLsZ5Pk"; out.Stored != want {
		t.Errorf("stored %q, want the Topic upload %q", out.Stored, want)
	}
}

func remixMBIDCase(resolution *eval.Resolution, isrc string) eval.Case {
	return eval.Case{
		ID: "t", Class: "RW",
		Track: eval.Track{Title: "Drinking in L.A.", Artist: "Bran Van 3000", Duration: 236, ISRC: isrc, Resolution: resolution},
		Candidates: []eval.Candidate{
			{Title: "Drinking in L.A.", URL: "album", Channel: "Bran Van 3000 - Topic", Duration: 237, AcoustID: "album-ac", RecordingMBIDs: []string{"album"}, Correct: true},
			{Title: "Drinking in L.A. (Who?)", URL: "remix", Channel: "Bran Van 3000 - Topic", Duration: 307, AcoustID: "remix-ac", RecordingMBIDs: []string{"remix"}},
		},
	}
}

func otherRecordingResults(acoustID, mbid, title string, duration float64) []eval.AcoustIDResult {
	return []eval.AcoustIDResult{{ID: acoustID, Score: 1, Recordings: []eval.LinkedRecording{{MBID: mbid, Title: title, Artists: []string{"Bran Van 3000"}, Duration: duration}}}}
}

func TestRun_ResolvedSearchMBIDRejectsAnUploadTheFingerprintLinksToAnotherSong(t *testing.T) {
	kase := remixMBIDCase(&eval.Resolution{Search: []eval.SearchedRecording{{Title: "Drinking in L.A.", Artist: "Bran Van 3000", MBID: "remix", Duration: 236}}}, "")
	kase.Candidates[0].AcoustIDResults = otherRecordingResults("album-ac", "album", "Different Song", 237)
	kase.Candidates[0].Title = "Drinking in L.A. (Radio Edit)"

	out := eval.Run(context.Background(), kase)

	if !out.Failed {
		t.Fatalf("expected the fingerprint's other-song result and the radio-edit title together to reject the album upload, stored %q", out.Stored)
	}
}

func TestRun_ISRCAnswerAloneSuppliesTheIdentity(t *testing.T) {
	kase := remixMBIDCase(&eval.Resolution{ISRCRecordings: []eval.ISRCRecording{{MBID: "remix", Duration: 307}}}, "CAA509814003")
	kase.Track.Duration = 0

	out := eval.Run(context.Background(), kase)

	if out.Stored != "remix" {
		t.Fatalf("stored %q, want the remix the ISRC answer names (its MBID and 307s length)", out.Stored)
	}
}

func TestRun_CaseWithoutResolutionCopiesItsIdentity(t *testing.T) {
	kase := remixMBIDCase(nil, "")
	kase.Track.MBID = "remix"
	kase.Candidates[0].AcoustIDResults = otherRecordingResults("album-ac", "album", "Different Song", 237)
	kase.Candidates[0].Title = "Drinking in L.A. (Radio Edit)"

	out := eval.Run(context.Background(), kase)

	if !out.Failed {
		t.Fatalf("expected the fingerprint's other-song result and the radio-edit title together to reject the album upload, stored %q", out.Stored)
	}
}

func TestRun_MarksAPendingCase(t *testing.T) {
	kase := remixMBIDCase(nil, "")
	kase.Pending = "selection vetoes unrequested versions"

	out := eval.Run(context.Background(), kase)

	if !out.Pending {
		t.Error("expected a case naming an owning ticket to be reported as pending")
	}
}

func TestRun_ScoredCaseIsNotPending(t *testing.T) {
	out := eval.Run(context.Background(), remixMBIDCase(nil, ""))

	if out.Pending {
		t.Error("expected a case with no owning ticket to be scored, not pending")
	}
}

func TestSummarize_KeepsPendingOutOfTheScore(t *testing.T) {
	outcomes := []eval.Outcome{
		{Case: eval.Case{ID: "a", Class: "F1"}, Pass: true},
		{Case: eval.Case{ID: "b", Class: "F1", Pending: "t1"}, Pending: true, Reason: "wrong"},
		{Case: eval.Case{ID: "c", Class: "RW", Pending: "t2"}, Pending: true, Pass: true},
	}

	r := eval.Summarize(outcomes)

	if r.Total != 1 || r.Passed != 1 {
		t.Fatalf("total/passed = %d/%d, want 1/1", r.Total, r.Passed)
	}
	if len(r.Classes) != 1 || r.Classes[0] != (eval.ClassResult{Class: "F1", Total: 1, Passed: 1}) {
		t.Fatalf("classes = %+v, want only F1 at 1/1", r.Classes)
	}
	if len(r.Failures) != 0 {
		t.Fatalf("failures = %+v, want none", r.Failures)
	}
	if len(r.Pending) != 2 || r.Pending[0].Case.ID != "b" || r.Pending[1].Case.ID != "c" {
		t.Fatalf("pending = %+v, want b and c", r.Pending)
	}
}

func timed(id string, seconds float64, attempts int) eval.Outcome {
	return eval.Outcome{Case: eval.Case{ID: id, Class: "OK"}, Pass: true, SimulatedSeconds: seconds, Attempts: attempts}
}

func TestSummarize_TakesTheMedianSecondsAndMeanAttemptsOfTheScoredCases(t *testing.T) {
	pending := timed("pending", 1000, 9)
	pending.Pending = true
	tests := []struct {
		name         string
		outcomes     []eval.Outcome
		wantMedian   float64
		wantAttempts float64
	}{
		{"an even count averages the middle two", []eval.Outcome{timed("a", 3, 1), timed("b", 1, 2), timed("c", 10, 2), timed("d", 2, 3), pending}, 2.5, 2},
		{"an odd count takes the middle one", []eval.Outcome{timed("a", 7, 1), timed("b", 1, 1), timed("c", 4, 4), pending}, 4, 2},
		{"nothing scored is zero", []eval.Outcome{pending}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := eval.Summarize(tt.outcomes)

			if r.MedianSimulatedSeconds != tt.wantMedian || r.MeanAttempts != tt.wantAttempts {
				t.Errorf("median %v s, mean %v attempts; want %v s and %v attempts, with the pending case left out", r.MedianSimulatedSeconds, r.MeanAttempts, tt.wantMedian, tt.wantAttempts)
			}
		})
	}
}

func TestRender_ShowsAccuracyAsAPercentage(t *testing.T) {
	got := eval.Summarize([]eval.Outcome{
		{Case: eval.Case{ID: "a", Class: "F1"}, Pass: true},
		{Case: eval.Case{ID: "b", Class: "F1"}, Reason: "wrong"},
		{Case: eval.Case{ID: "c", Class: "F2"}, Pass: true},
		{Case: eval.Case{ID: "d", Class: "F2"}, Pass: true},
	}).Render()

	for _, want := range []string{"3/4 (75.0%)", "F1       1/2      50%", "F2       2/2      100%"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() missing %q:\n%s", want, got)
		}
	}
}

func TestRender_ListsPendingCasesWithTheirOwner(t *testing.T) {
	r := eval.Summarize([]eval.Outcome{
		{Case: eval.Case{ID: "a", Class: "F1"}, Pass: true},
		{Case: eval.Case{ID: "mh-fail", Class: "RW", Pending: "owner one"}, Pending: true, Reason: "stored the wrong recording"},
		{Case: eval.Case{ID: "mh-pass", Class: "RW", Pending: "owner two"}, Pending: true, Pass: true},
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
	got := eval.Summarize([]eval.Outcome{{Case: eval.Case{ID: "a", Class: "F1"}, Pass: true}}).Render()

	if strings.Contains(got, "Pending") {
		t.Errorf("Render() printed a pending section with nothing pending:\n%s", got)
	}
}

func TestRunAll_ScoresEmbeddedMustHoldsInsteadOfLeavingThemPending(t *testing.T) {
	cases, err := eval.LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	want := map[string]bool{
		"mh4-unknown-fingerprint-topic-within-2s-stored-best-effort": true,
		"mh4-unknown-fingerprint-non-topic-20s-off-fails":            true,
		"mh11-radio-edit-loses-to-clean-topic-upload":                true,
		"mh11-radio-edit-only-with-wrong-length-fails":               true,
	}

	for _, o := range eval.RunAll(context.Background(), cases) {
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
	cases, err := eval.LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}

	for _, c := range cases {
		if c.Pending != "" && strings.TrimSpace(c.Pending) == "" {
			t.Errorf("pending case %q names no owning ticket", c.ID)
		}
	}
}

func TestRunAll_ScoresDrinkingInLAInTheRealWorldClass(t *testing.T) {
	cases, err := eval.LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}

	report := eval.Summarize(eval.RunAll(context.Background(), cases))

	var rw *eval.ClassResult
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
	kase := eval.Case{
		ID: "t", Class: "RW",
		Track: eval.Track{
			Title: "Drinking in L.A.", Artist: "Bran Van 3000", Duration: 236, ISRC: "CAA509814003",
			Resolution: &eval.Resolution{
				Search: []eval.SearchedRecording{{Title: "Drinking in L.A.", Artist: "Bran Van 3000", MBID: "remix", ISRC: "CAA509814003", Duration: 236}},
				ISRCRecordings: []eval.ISRCRecording{
					{MBID: "edit", Duration: 220},
					{MBID: "album", Duration: 236},
				},
			},
		},
		Candidates: []eval.Candidate{
			{Title: "Drinking in L.A. (edit)", URL: "edit", Channel: "Bran Van 3000 - Topic", Duration: 236, AcoustID: "edit-ac", RecordingMBIDs: []string{"edit"}},
			{Title: "Drinking in L.A.", URL: "album", Channel: "Bran Van 3000 - Topic", Duration: 237, AcoustID: "album-ac", RecordingMBIDs: []string{"album"}, Correct: true},
		},
	}

	out := eval.Run(context.Background(), kase)

	if out.Stored != "album" {
		t.Fatalf("stored %q (reason %q), want the album recording whose 236s length matches the track", out.Stored, out.Reason)
	}
}

func TestRegressions_IgnoresAFailingPendingCase(t *testing.T) {
	report := eval.Summarize([]eval.Outcome{
		{Case: eval.Case{ID: "a", Class: "RW"}, Pass: true},
		{Case: eval.Case{ID: "b", Class: "RW", Pending: "selection vetoes unrequested versions"}, Pending: true, Stored: "instrumental", Reason: "stored the wrong recording"},
		{Case: eval.Case{ID: "c", Class: "F1", Pending: "compute acquisition confidence from evidence"}, Pending: true, Failed: true, Reason: "nothing stored"},
	})
	base := eval.Baseline{Accuracy: 1.0, Classes: map[string]float64{"RW": 1.0}}

	if got := report.Regressions(base); len(got) != 0 {
		t.Fatalf("failing pending cases moved the baseline gate: %v", got)
	}
}

func TestRun_EmptyResolutionStillAcquiresTheTrack(t *testing.T) {
	kase := eval.Case{
		ID: "t", Class: "OK",
		Track:      eval.Track{Title: "Circles", Artist: "Post Malone", Duration: 215, Resolution: &eval.Resolution{}},
		Candidates: []eval.Candidate{{Title: "Circles", URL: "u", Channel: "Post Malone - Topic", Duration: 215, Correct: true}},
	}

	out := eval.Run(context.Background(), kase)

	if out.Stored != "u" {
		t.Fatalf("stored %q (reason %q, err %q), want the only clean upload", out.Stored, out.Reason, out.Err)
	}
}

func TestRun_TwoNamedSourcesMergeThroughTheRealRegistry(t *testing.T) {
	kase := eval.Case{
		ID: "t", Class: "OK",
		Track: eval.Track{Title: "Solitude", Artist: "Nova", Duration: 200},
		Sources: []eval.Source{
			{Name: "ytmusic", Candidates: []eval.Candidate{
				{Title: "Solitude (Cover)", URL: "cover", Channel: "Cover Channel", Duration: 200},
			}},
			{Name: "ytdlp", Candidates: []eval.Candidate{
				{Title: "Solitude", URL: "master", Channel: "Nova - Topic", Duration: 200, Correct: true},
			}},
		},
	}

	out := eval.Run(context.Background(), kase)

	if !out.Pass {
		t.Fatalf("expected pass, got %q (stored %q)", out.Reason, out.Stored)
	}
	if out.Stored != "master" {
		t.Errorf("stored %q, want the ytdlp source's master over ytmusic's cover", out.Stored)
	}
}

func TestRun_CandidateBehindAnAbsentQueryVariantNeverBecomesACandidate(t *testing.T) {
	kase := eval.Case{
		ID: "t", Class: "OK",
		Track: eval.Track{Title: "Quiet Static", Artist: "Nova", Duration: 190},
		Sources: []eval.Source{{Name: "ytdlp", Candidates: []eval.Candidate{
			{Title: "Quiet Static", URL: "album-edition", Channel: "Nova - Topic", Duration: 190, ViewCount: 900000, Query: eval.QueryTitleArtistAlbum},
			{Title: "Quiet Static", URL: "master", Channel: "Nova - Topic", Duration: 190, ViewCount: 100, Query: eval.QueryTitleArtist, Correct: true},
		}}},
	}

	out := eval.Run(context.Background(), kase)

	if !out.Pass {
		t.Fatalf("expected pass, got %q (stored %q): the title_artist_album candidate must never surface without an album on the track", out.Reason, out.Stored)
	}
	if out.Stored != "master" {
		t.Errorf("stored %q, want master: the higher-view candidate behind an absent query variant must never be found", out.Stored)
	}
}

func TestRun_ACandidateBehindTheAlbumOrISRCQueryIsFoundOnlyWhenTheTrackHasOne(t *testing.T) {
	behind := func(query string, track eval.Track) eval.Case {
		return eval.Case{
			ID: "t", Class: "OK",
			Track: track,
			Sources: []eval.Source{{Name: "ytdlp", Candidates: []eval.Candidate{
				{Title: "Solitude", URL: "behind-" + query, Channel: "Nova - Topic", Duration: 200, Query: query, Correct: true},
			}}},
		}
	}
	bare := eval.Track{Title: "Solitude", Artist: "Nova", Duration: 200}
	withAlbum, withISRC := bare, bare
	withAlbum.Album, withISRC.ISRC = "Nightfall", "USAAA2400001"
	tests := []struct {
		name   string
		kase   eval.Case
		stored string
	}{
		{"the album query without an album", behind(eval.QueryTitleArtistAlbum, bare), ""},
		{"the album query with an album", behind(eval.QueryTitleArtistAlbum, withAlbum), "behind-" + eval.QueryTitleArtistAlbum},
		{"the isrc query without an isrc", behind(eval.QueryISRC, bare), ""},
		{"the isrc query with an isrc", behind(eval.QueryISRC, withISRC), "behind-" + eval.QueryISRC},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := eval.Run(context.Background(), tt.kase)

			if out.Stored != tt.stored {
				t.Errorf("stored %q (%s), want %q", out.Stored, out.Reason, tt.stored)
			}
		})
	}
}

func TestRun_SimulatedSecondsAndAttemptsAccumulateAlongThePath(t *testing.T) {
	kase := eval.Case{
		ID: "t", Class: "OK",
		Track: eval.Track{Title: "Solitude", Artist: "Nova", Duration: 200},
		Sources: []eval.Source{{Name: "ytdlp", SearchSeconds: 7, Candidates: []eval.Candidate{
			{Title: "Solitude", URL: "master", Channel: "Nova - Topic", Duration: 200, DownloadSeconds: 30, Correct: true},
		}}},
	}

	out := eval.Run(context.Background(), kase)

	if out.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", out.Attempts)
	}
	if out.SimulatedSeconds != 37 {
		t.Fatalf("simulated seconds = %.1f, want 37 (7s search + 30s download)", out.SimulatedSeconds)
	}
}

func TestRun_FallbackTimeAddsToTheFirstTier(t *testing.T) {
	kase := eval.Case{
		ID: "t", Class: "OK",
		Track: eval.Track{Title: "Solitude", Artist: "Nova", Duration: 200},
		Sources: []eval.Source{
			{Name: "ytmsearch", SearchSeconds: 3, Candidates: []eval.Candidate{
				{Title: "Solitude", URL: "art", Duration: 200, Resolved: true, Kind: "art_track", Artists: []string{"Nova"}, DownloadFails: true, DownloadSeconds: 5},
			}},
			{Name: "ytdlp", SearchSeconds: 4, Candidates: []eval.Candidate{
				{Title: "Solitude", URL: "master", Channel: "Nova - Topic", Duration: 200, DownloadSeconds: 7, Correct: true},
			}},
		},
	}

	out := eval.Run(context.Background(), kase)

	if out.Stored != "master" {
		t.Fatalf("stored %q, want master from the fallback tier", out.Stored)
	}
	if out.SimulatedSeconds != 19 {
		t.Fatalf("simulated seconds = %.1f, want 19 (3s + 4s of search, 5s + 7s of download)", out.SimulatedSeconds)
	}
}

func TestLoadDir_RefusesTimesBeyondProdTimeouts(t *testing.T) {
	track := eval.Track{Title: "t", Artist: "a"}
	tests := []struct {
		name string
		kase eval.Case
	}{
		{"a candidate slower than the candidate timeout", eval.Case{ID: "x", Class: "F1", Track: track, Candidates: []eval.Candidate{
			{URL: "u", DownloadSeconds: service.DefaultCandidateTimeout.Seconds() + 1},
		}}},
		{"a case slower than the job timeout", eval.Case{ID: "x", Class: "F1", Track: track, Sources: []eval.Source{
			{Name: "ytmsearch", SearchSeconds: 300, Candidates: []eval.Candidate{{URL: "u", DownloadSeconds: 170}}},
			{Name: "ytdlp", SearchSeconds: 300, Candidates: []eval.Candidate{{URL: "v", DownloadSeconds: 170}}},
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validate(t, []eval.Case{tt.kase}); err == nil {
				t.Fatal("LoadDir = nil, want a refusal")
			}
		})
	}
}

func TestRegressions_FlagsSimulatedSecondsAndAttemptsAboveBaseline(t *testing.T) {
	r := eval.Report{Total: 1, Passed: 1, MedianSimulatedSeconds: 110, MeanAttempts: 2.2}
	base := eval.Baseline{Accuracy: 1.0, MedianSeconds: 100, MeanAttempts: 2.0}

	got := r.Regressions(base)
	if len(got) != 2 {
		t.Fatalf("expected a median-seconds and a mean-attempts regression, got %v", got)
	}
}

func TestRegressions_SilentWithinTheFivePercentMargin(t *testing.T) {
	r := eval.Report{Total: 1, Passed: 1, MedianSimulatedSeconds: 104, MeanAttempts: 2.05}
	base := eval.Baseline{Accuracy: 1.0, MedianSeconds: 100, MeanAttempts: 2.0}

	if got := r.Regressions(base); len(got) != 0 {
		t.Fatalf("expected no regression within the 5%% margin, got %v", got)
	}
}

func TestLoadDir_RejectsEveryBidiControlInACandidateTitle(t *testing.T) {
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
			kase := eval.Case{ID: "bidi-case", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Candidates: []eval.Candidate{{URL: "u", Title: "Halo " + string(r) + "oidoV"}}}
			if err := validate(t, []eval.Case{kase}); err == nil {
				t.Errorf("LoadDir accepted candidate title %+q, want rejection", kase.Candidates[0].Title)
			}
		})
	}
}

func TestLoadDir_RejectsWhitespaceAndOtherControlsInTrackAndCandidateText(t *testing.T) {
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
			kase := eval.Case{ID: "ctl-case", Class: "F1", Track: eval.Track{Title: "Halo" + string(r), Artist: "a"}, Candidates: []eval.Candidate{{URL: "u"}}}
			if err := validate(t, []eval.Case{kase}); err == nil {
				t.Errorf("LoadDir accepted track title %+q, want rejection", kase.Track.Title)
			}
		})
		t.Run(name+" in candidate channel", func(t *testing.T) {
			kase := eval.Case{ID: "ctl-case", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Candidates: []eval.Candidate{{URL: "u", Channel: string(r) + "Topic"}}}
			if err := validate(t, []eval.Case{kase}); err == nil {
				t.Errorf("LoadDir accepted candidate channel %+q, want rejection", kase.Candidates[0].Channel)
			}
		})
	}
}

func TestLoadDir_AcceptsEmojiSequencesCombiningMarksAndFullwidthText(t *testing.T) {
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
			kase := eval.Case{
				ID: "unicode-case", Class: "OK",
				Track:      eval.Track{Title: text, Artist: text, Album: text},
				Candidates: []eval.Candidate{{URL: "u", Title: text, Channel: text}},
			}
			if err := validate(t, []eval.Case{kase}); err != nil {
				t.Errorf("LoadDir rejected ordinary text %+q: %v", text, err)
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
			_, err := eval.LoadDir(dir)
			if err == nil {
				t.Fatalf("LoadDir accepted golden %s, want rejection", body)
			}
			if !strings.Contains(err.Error(), `"json-esc"`) {
				t.Errorf("LoadDir error %q should name case %q", err, "json-esc")
			}
		})
	}
}

func TestLoadDir_NamesTheCaseAndFieldForArtistAlbumAndSourceCandidates(t *testing.T) {
	tests := []struct {
		name      string
		kase      eval.Case
		wantField string
	}{
		{"track artist", eval.Case{ID: "artist-case", Class: "F1", Track: eval.Track{Title: "t", Artist: "a\x1b"}, Candidates: []eval.Candidate{{URL: "u"}}}, "artist"},
		{"track album", eval.Case{ID: "album-case", Class: "F1", Track: eval.Track{Title: "t", Artist: "a", Album: string(rune(0x2067)) + "b"}, Candidates: []eval.Candidate{{URL: "u"}}}, "album"},
		{"source candidate channel", eval.Case{ID: "source-case", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Sources: []eval.Source{{Name: "s", Candidates: []eval.Candidate{{URL: "u", Channel: "c" + string(rune(0x202E))}}}}}, "channel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(t, []eval.Case{tt.kase})
			if err == nil {
				t.Fatal("expected validation to reject this suite")
			}
			if msg := err.Error(); !strings.Contains(msg, `"`+tt.kase.ID+`"`) || !strings.Contains(msg, tt.wantField) {
				t.Errorf("error %q should name case %q and field %q", msg, tt.kase.ID, tt.wantField)
			}
		})
	}
}

func expectationCase(provenance, failureCode string, candidate eval.Candidate) eval.Case {
	return eval.Case{
		ID: "t", Class: "F0", ExpectProvenance: provenance, ExpectFailureCode: failureCode,
		Track:      eval.Track{Title: "Night Drive", Artist: "Halcyon Ferry", Duration: 204},
		Candidates: []eval.Candidate{candidate},
	}
}

func TestRun_JudgesTheExpectedProvenanceAndFailureCode(t *testing.T) {
	stored := eval.Candidate{Title: "Night Drive", URL: "u", Channel: "Halcyon Ferry - Topic", Duration: 205, Correct: true}
	rejected := eval.Candidate{Title: "Cooking Tutorial Episode 47", URL: "cook", Channel: "Cooking", Duration: 204}
	probe := eval.Run(context.Background(), expectationCase("", "", stored))
	failProbe := eval.Run(context.Background(), expectationCase("", "", rejected))
	if probe.Failed || probe.Provenance == "" || !failProbe.Failed || failProbe.FailureCode == "" {
		t.Fatalf("probe runs did not store/fail as assumed: %+v %+v", probe, failProbe)
	}
	wrongProvenance := "verified"
	if probe.Provenance == wrongProvenance {
		wrongProvenance = "best_effort"
	}
	tests := []struct {
		name string
		kase eval.Case
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
			out := eval.Run(context.Background(), tc.kase)
			if out.Pass != tc.pass {
				t.Errorf("pass = %v (%s), want %v", out.Pass, out.Reason, tc.pass)
			}
		})
	}
}

func TestLoadEmbedded_RejectsUnknownExpectedProvenance(t *testing.T) {
	kase := expectationCase("maybe", "", eval.Candidate{URL: "u"})
	if err := validate(t, []eval.Case{kase}); err == nil {
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
		if kase.ExpectProvenance != exp[0] || kase.ExpectFailureCode != exp[1] || kase.Pending != "" {
			t.Errorf("%s: got %q/%q pending=%v", id, kase.ExpectProvenance, kase.ExpectFailureCode, kase.Pending != "")
		}
	}
}

func TestRunAll_OverlappedDownloadsScoreAsTheSlowestOfTheWindow(t *testing.T) {
	kase := eval.Case{
		ID: "t", Class: "OK",
		Track: eval.Track{Title: "Solitude", Artist: "Nova", Duration: 200},
		Sources: []eval.Source{{Name: "ytdlp", SearchSeconds: 0.5, Candidates: []eval.Candidate{
			{Title: "Solitude", URL: "master", Channel: "Nova - Topic", Duration: 200, DownloadSeconds: 20, Correct: true},
			{Title: "Solitude", URL: "second", Channel: "Nova Fan", Duration: 200, DownloadSeconds: 30},
		}}},
	}

	out := eval.RunAll(context.Background(), []eval.Case{kase})[0]

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
	cases, err := eval.LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	var kase eval.Case
	for _, c := range cases {
		if c.ID == "pl-art-track-by-the-searched-artist-is-trusted-without-a-fingerprint" {
			kase = c
		}
	}

	out := eval.Run(context.Background(), kase)

	if !out.Pass {
		t.Fatalf("expected pass, got %q (stored %q)", out.Reason, out.Stored)
	}
}

func artTrackCase(track eval.Track, candidates ...eval.Candidate) eval.Case {
	return eval.Case{ID: "pins", Class: "RW", Track: track, Candidates: candidates}
}

func artTrack(url string, seconds float64, explicit bool) eval.Candidate {
	return eval.Candidate{Title: "t", URL: url, Duration: seconds, Resolved: true, Kind: "art_track", Explicit: explicit, Correct: true}
}

func TestRun_TheCatalogLengthNotTheTrackLengthIsTheTarget(t *testing.T) {
	track := eval.Track{Title: "t", Artist: "a", Duration: 167, CatalogDuration: 139}

	if out := eval.Run(context.Background(), artTrackCase(track, artTrack("catalog-length", 139, false))); !out.Pass || out.Stored != "catalog-length" {
		t.Errorf("stored %q (%s), want the 139 s candidate the catalog length asks for", out.Stored, out.Reason)
	}
	if out := eval.Run(context.Background(), artTrackCase(track, artTrack("track-length", 167, false))); out.Stored != "" {
		t.Errorf("stored %q, want the 167 s candidate refused against the 139 s catalog length", out.Stored)
	}
}

func TestRun_TheCatalogExplicitFlagPicksBetweenTheCleanAndExplicitTwins(t *testing.T) {
	clean, explicit := false, true
	tests := []struct {
		name    string
		catalog *bool
		want    string
	}{
		{"a clean catalog flag stores the clean twin", &clean, "clean"},
		{"an explicit catalog flag stores the explicit twin", &explicit, "explicit"},
		{"no catalog flag stores the explicit twin", nil, "explicit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			track := eval.Track{Title: "t", Artist: "a", CatalogDuration: 139, CatalogExplicit: tt.catalog}

			out := eval.Run(context.Background(), artTrackCase(track, artTrack("clean", 139, false), artTrack("explicit", 139, true)))

			if out.Stored != tt.want {
				t.Errorf("stored %q (%s), want the %s twin", out.Stored, out.Reason, tt.want)
			}
		})
	}
}

func TestRun_TheShadowJudgeSeesEachCandidatesAlbum(t *testing.T) {
	tests := []struct {
		name    string
		album   string
		verdict string
	}{
		{"a candidate on the track's album is accepted", "Unapologetic", "verdict=agree"},
		{"a candidate with no album cannot confirm the cut", "", "verdict=judge_abstained"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logged := capturedLog(t)
			candidate := artTrack("art-track", 200, false)
			candidate.Album, candidate.Artists = tt.album, []string{"a"}

			out := eval.Run(context.Background(), artTrackCase(eval.Track{Title: "t", Artist: "a", Album: "Unapologetic"}, candidate))

			if out.Stored != "art-track" {
				t.Fatalf("stored %q (%s), want the art track", out.Stored, out.Reason)
			}
			if !strings.Contains(logged.String(), tt.verdict) {
				t.Errorf("shadow log = %q, want %s: with no length to go on, only the album confirms the cut", logged.String(), tt.verdict)
			}
		})
	}
}

func TestRun_TheCatalogPinAloneVerifiesTheStoredUpload(t *testing.T) {
	kase := embeddedCase(t, "rw-everybody-wants-pin-verifies-the-stored-upload")

	out := eval.Run(context.Background(), kase)

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
	kase := eval.Case{
		ID: "t", Class: "RW",
		Track: eval.Track{Title: "Song", Artist: "Artist", Duration: 200, CatalogMBID: "pin", Resolution: &eval.Resolution{}},
		Candidates: []eval.Candidate{
			{Title: "Song", URL: "other-version", Channel: "Artist - Topic", Duration: 200, ViewCount: 9000000, AcoustID: "other-ac", RecordingMBIDs: []string{"other"}, AcoustIDResults: []eval.AcoustIDResult{{ID: "other-ac", Score: 1, Recordings: []eval.LinkedRecording{{MBID: "other", Title: "Another Song", Artists: []string{"Artist"}, Duration: 200}}}}},
			{Title: "Song", URL: "pinned", Channel: "Artist - Topic", Duration: 200, ViewCount: 10, AcoustID: "pin-ac", RecordingMBIDs: []string{"pin"}, Correct: true},
		},
	}

	out := eval.Run(context.Background(), kase)

	if !out.Pass || out.Stored != "pinned" {
		t.Errorf("pass %v stored %q (%s), want the pinned recording's upload", out.Pass, out.Stored, out.Reason)
	}
}

func TestLoadDir_RejectsAPinnedRecordingTheCatalogDoesNotPin(t *testing.T) {
	pinned := &eval.PinnedRecording{MBID: "pin"}
	tests := []struct {
		name        string
		catalogMBID string
	}{
		{"no catalog pin", ""},
		{"a different catalog pin", "other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cases := []eval.Case{{
				ID: "x", Class: "RW",
				Track:      eval.Track{Title: "t", Artist: "a", CatalogMBID: tt.catalogMBID, Resolution: &eval.Resolution{Pinned: pinned}},
				Candidates: []eval.Candidate{{URL: "u"}},
			}}

			if err := validate(t, cases); err == nil {
				t.Error("LoadDir accepted a recorded pinned recording the catalog does not pin")
			}
		})
	}
}

func TestRun_AYTMusicAdapterSourceEmitsTheCatalogPinnedVideoFirst(t *testing.T) {
	pinned := "https://music.youtube.com/watch?v=OkjmSTJOAL4"
	kase := eval.Case{
		ID: "pin", Class: "RW",
		Track: eval.Track{
			Title: "Mubu", Artist: "LUCKI", Duration: 156,
			CatalogSources: []eval.CatalogSource{{Provider: "youtube", ExternalID: "OkjmSTJOAL4"}},
		},
		Sources: []eval.Source{{Name: "ytmusic", Adapter: "ytmusic", Candidates: []eval.Candidate{
			{Title: "Mubu", URL: pinned, Artists: []string{"LUCKI"}, ActualDuration: 156, Correct: true},
		}}},
	}

	out := eval.Run(context.Background(), kase)

	if !out.Pass || out.Stored != pinned {
		t.Fatalf("stored %q (%s), want the catalog-pinned video", out.Stored, out.Reason)
	}

	kase.Track.CatalogSources = nil
	if out := eval.Run(context.Background(), kase); out.Pass {
		t.Errorf("without a catalog pin the adapter has no video to emit; stored %q", out.Stored)
	}
}

func TestLoadDir_RejectsAnUnknownSourceAdapter(t *testing.T) {
	kase := eval.Case{
		ID: "a", Class: "RW", Track: eval.Track{Title: "t", Artist: "a"},
		Sources: []eval.Source{{Name: "x", Adapter: "nope", Candidates: []eval.Candidate{{URL: "u"}}}},
	}

	if err := validate(t, []eval.Case{kase}); err == nil {
		t.Error("expected an unknown adapter to be rejected")
	}
}

func TestRun_ACatalogPinAdapterSourceEmitsThePickedSoundCloudUpload(t *testing.T) {
	picked := "https://soundcloud.com/curre-2/chicago-finale-mp3"
	kase := eval.Case{
		ID: "pin", Class: "RW",
		Track: eval.Track{
			Title: "Street Player 2021 - Radio Edit", Artist: "D.J.F.",
			CatalogSources: []eval.CatalogSource{{Provider: "soundcloud", ExternalID: "1506924427", URL: picked}},
		},
		Sources: []eval.Source{{Name: "catalogpin", Adapter: "catalogpin", Candidates: []eval.Candidate{
			{Title: "CURRE, D.J.F - Street Player 2021 (Edit)", URL: picked, Duration: 192.124, Correct: true},
		}}},
	}

	out := eval.Run(context.Background(), kase)

	if !out.Pass || out.Stored != picked {
		t.Fatalf("stored %q (%s), want the picked upload", out.Stored, out.Reason)
	}

	kase.Track.CatalogSources = nil
	if out := eval.Run(context.Background(), kase); out.Pass {
		t.Errorf("without a catalog pin the adapter emits nothing; stored %q", out.Stored)
	}
}

func TestRun_AFingerprintNamingTheTracksRecordingVerifiesAnUpload(t *testing.T) {
	upload := func(fingerprint func(*eval.Candidate)) eval.Case {
		cand := eval.Candidate{Title: "Song", URL: "upload", Channel: "a fan", Duration: 200, Kind: "upload", Correct: true}
		fingerprint(&cand)
		return eval.Case{ID: "fp", Class: "OK", Track: eval.Track{Title: "Song", Artist: "Artist", Duration: 200, MBID: "song"}, Candidates: []eval.Candidate{cand}}
	}
	tests := []struct {
		name        string
		fingerprint func(*eval.Candidate)
	}{
		{"from its recording ids alone", func(c *eval.Candidate) { c.AcoustID, c.RecordingMBIDs = "song-ac", []string{"song"} }},
		{"from its AcoustID results alone", func(c *eval.Candidate) {
			c.AcoustIDResults = []eval.AcoustIDResult{{ID: "song-ac", Score: 1, Recordings: []eval.LinkedRecording{{MBID: "song", Title: "Song", Artists: []string{"Artist"}, Duration: 200}}}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := eval.Run(context.Background(), upload(tt.fingerprint))

			if out.Stored != "upload" || out.Provenance != "verified" {
				t.Errorf("stored %q with provenance %q (%s), want the upload verified by its fingerprint", out.Stored, out.Provenance, out.Reason)
			}
		})
	}

	t.Run("without a fingerprint", func(t *testing.T) {
		out := eval.Run(context.Background(), upload(func(*eval.Candidate) {}))

		if out.Stored != "" || out.FailureCode != "no_confident_match" {
			t.Errorf("stored %q, failure %q; want no_confident_match for an unfingerprinted upload", out.Stored, out.FailureCode)
		}
	})
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

		out := eval.Run(context.Background(), kase)

		if !out.Pass {
			t.Errorf("%s: expected pass, got %q (err %q)", id, out.Reason, out.Err)
		}
	}
}

func TestRun_SavesTheISRCAndTheMeasuredLengthThroughTheLedger(t *testing.T) {
	out := eval.Run(context.Background(), embeddedCase(t, "rw-drinking-in-la-remix-mbid-anchored-to-isrc"))

	want := eval.SavedTrack{ISRC: "CAA509814003", ISRCSource: "catalog", Provenance: "best_effort", DurationSeconds: 237}
	if out.Saved.ISRC != want.ISRC || out.Saved.ISRCSource != want.ISRCSource ||
		out.Saved.Provenance != want.Provenance || out.Saved.DurationSeconds != want.DurationSeconds {
		t.Errorf("saved %+v, want %+v", out.Saved, want)
	}
}

func TestRun_AdoptsTheISRCTheFingerprintProves(t *testing.T) {
	const mbid = "aaaaaaaa-0000-4000-8000-000000000001"
	kase := eval.Case{
		ID: "adopt", Class: "RW",
		Track: eval.Track{
			Title: "Song", Artist: "Artist", Duration: 200,
			Resolution: &eval.Resolution{Search: []eval.SearchedRecording{{Title: "Song", Artist: "Artist", MBID: mbid, ISRC: "CAA509814003", Duration: 200}}},
		},
		Candidates: []eval.Candidate{{
			Title: "Song", URL: "u", Channel: "Artist - Topic", Duration: 200, Correct: true,
			AcoustID: "ac", RecordingMBIDs: []string{mbid},
		}},
	}

	out := eval.Run(context.Background(), kase)

	if out.Saved.ISRC != "CAA509814003" || out.Saved.ISRCSource != "fingerprint" {
		t.Errorf("saved %+v, want the fingerprint-proven ISRC written back", out.Saved)
	}
}

func TestRun_TheRecordedSearchKeepsEachHitsOwnTitle(t *testing.T) {
	const live, studio = "aaaaaaaa-0000-4000-8000-00000000000a", "aaaaaaaa-0000-4000-8000-00000000000b"
	kase := eval.Case{
		ID: "live-first", Class: "RW",
		Track: eval.Track{
			Title: "Song", Artist: "Artist", Duration: 200,
			Resolution: &eval.Resolution{Search: []eval.SearchedRecording{
				{Title: "Song (Live)", Artist: "Artist", MBID: live, Duration: 250},
				{Title: "Song", Artist: "Artist", MBID: studio, Duration: 200},
			}},
		},
		Candidates: []eval.Candidate{{
			Title: "Song", URL: "u", Channel: "Artist - Topic", Duration: 200, Correct: true,
			AcoustID: "ac", RecordingMBIDs: []string{studio},
		}},
	}

	out := eval.Run(context.Background(), kase)

	if out.Failed || out.Stored != "u" {
		t.Fatalf("stored %q failed=%v err=%q, want the upload linked to the studio recording", out.Stored, out.Failed, out.Err)
	}
}

func TestRun_JudgesTheExpectedSavedTrack(t *testing.T) {
	base := embeddedCase(t, "rw-drinking-in-la-remix-mbid-anchored-to-isrc")
	tests := []struct {
		name string
		want eval.SavedExpectation
		fail string
	}{
		{"isrc", eval.SavedExpectation{ISRC: "XX0000000000"}, "want saved isrc XX0000000000"},
		{"isrc source", eval.SavedExpectation{ISRCSource: "fingerprint"}, "want saved isrc source fingerprint"},
		{"provenance", eval.SavedExpectation{Provenance: "verified"}, "want saved provenance verified"},
		{"duration", eval.SavedExpectation{DurationSeconds: 236}, "want saved duration 236"},
		{"rejected keys", eval.SavedExpectation{RejectedKeys: []string{"youtube:abc"}}, "want saved rejected keys [youtube:abc]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kase := base
			kase.ExpectSaved = &tt.want

			out := eval.Run(context.Background(), kase)

			if out.Pass || !strings.HasPrefix(out.Reason, tt.fail) {
				t.Errorf("pass=%v reason=%q, want a failure starting %q", out.Pass, out.Reason, tt.fail)
			}
		})
	}
}

func TestRun_ReplacingAFileOfUnknownSourceAimsAtTheCatalogNotTheStoredLength(t *testing.T) {
	out := eval.Run(context.Background(), embeddedCase(t, "pl-replace-measured-length-is-not-the-target"))

	if !out.Pass {
		t.Fatalf("expected pass, got %q (err %q)", out.Reason, out.Err)
	}
	if want := "https://www.youtube.com/watch?v=fV3KcFFVZJ8"; out.Stored != want {
		t.Errorf("stored %q, want the artist's upload %q", out.Stored, want)
	}
}

func TestRun_ReplaceNeverStoresTheFileTheTrackAlreadyHolds(t *testing.T) {
	kase := embeddedCase(t, "replace-unknown-source-never-stores-the-current-file-again")

	out := eval.Run(context.Background(), kase)

	if !out.Pass || out.Stored != "" {
		t.Errorf("pass=%v stored=%q reason=%q, want nothing stored", out.Pass, out.Stored, out.Reason)
	}
	unfingerprinted := kase
	unfingerprinted.Stored = &eval.StoredFile{Duration: kase.Stored.Duration}
	if out := eval.Run(context.Background(), unfingerprinted); out.Pass {
		t.Errorf("without a fingerprint of the stored file the upload wins and the case must fail; stored %q", out.Stored)
	}
}

func TestRun_ASeededRejectedSourceNeverComesBack(t *testing.T) {
	kase := embeddedCase(t, "reacquire-fails-rather-than-return-the-only-upload-the-user-rejected")

	out := eval.Run(context.Background(), kase)

	if !out.Pass || out.Stored != "" {
		t.Errorf("pass=%v stored=%q reason=%q, want the rejected upload refused", out.Pass, out.Stored, out.Reason)
	}
}

func TestRun_ATrackTheRunDidNotStoreKeepsItsCatalogLength(t *testing.T) {
	kase := eval.Case{
		ID: "t", Class: "F7",
		Track:      eval.Track{Title: "Save Your Tears", Artist: "The Weeknd", CatalogDuration: 215},
		Candidates: []eval.Candidate{{Title: "Cooking Tutorial Episode 47", URL: "cook", Channel: "Cooking", Duration: 215}},
	}

	out := eval.Run(context.Background(), kase)

	if !out.Failed || out.Stored != "" {
		t.Fatalf("failed=%v stored=%q, want the run to store nothing", out.Failed, out.Stored)
	}
	if out.Saved.DurationSeconds != 215 {
		t.Errorf("saved length %v, want the catalog's 215 s kept on a track that was never stored", out.Saved.DurationSeconds)
	}
}

func TestRun_TheTrackTitleIsTrimmedAsTheCatalogTrimsIt(t *testing.T) {
	kase := eval.Case{
		ID: "t", Class: "F7",
		Track:      eval.Track{Title: "  Save Your Tears ", Artist: "The Weeknd", Duration: 215},
		Candidates: []eval.Candidate{{Title: "Cooking Tutorial Episode 47", URL: "cook", Channel: "Cooking", Duration: 215}},
	}

	if got := eval.Run(context.Background(), kase).Saved.Title; got != "Save Your Tears" {
		t.Errorf("saved title %q, want %q: the catalog trims a new track's title", got, "Save Your Tears")
	}
}

func TestRun_TheRejectedKeyStaysOnTheSavedTrack(t *testing.T) {
	out := eval.Run(context.Background(), embeddedCase(t, "reacquire-never-returns-the-upload-the-user-rejected"))

	if want := []string{"youtube:FjkkqbEbFOY"}; !slices.Equal(out.Saved.RejectedKeys, want) {
		t.Errorf("saved rejected keys %v, want %v", out.Saved.RejectedKeys, want)
	}
}

func TestLoadDir_RejectsAStoredFileWithNoDuration(t *testing.T) {
	cases := []eval.Case{{
		ID: "x", Class: "OK", Stored: &eval.StoredFile{},
		Track:      eval.Track{Title: "t", Artist: "a"},
		Candidates: []eval.Candidate{{URL: "u"}},
	}}

	if err := validate(t, cases); err == nil || !strings.Contains(err.Error(), "stores a file with no duration") {
		t.Fatalf("LoadDir = %v, want a stored-file duration error", err)
	}
}

func TestLoadDir_RejectsATrackLengthNoCatalogWouldHold(t *testing.T) {
	week := float64(7 * 24 * 60 * 60)
	for name, c := range map[string]eval.Case{
		"negative track length":      {Track: eval.Track{Title: "t", Artist: "a", Duration: -1}},
		"catalog length past a week": {Track: eval.Track{Title: "t", Artist: "a", CatalogDuration: week + 1}},
		"track length past a week":   {Track: eval.Track{Title: "t", Artist: "a", Duration: week + 1}},
		"stored file past a week":    {Track: eval.Track{Title: "t", Artist: "a"}, Stored: &eval.StoredFile{Duration: week + 1}},
	} {
		t.Run(name, func(t *testing.T) {
			c.ID, c.Class, c.Candidates = "x", "OK", []eval.Candidate{{URL: "u"}}
			if err := validate(t, []eval.Case{c}); !errors.Is(err, shared.ErrImplausibleTrackLength) {
				t.Fatalf("LoadDir = %v, want the kernel's implausible track length error", err)
			}
		})
	}
	edge := eval.Case{ID: "x", Class: "OK", Track: eval.Track{Title: "t", Artist: "a", CatalogDuration: week}, Candidates: []eval.Candidate{{URL: "u"}}}
	if err := validate(t, []eval.Case{edge}); err != nil {
		t.Fatalf("LoadDir = %v, want a length of exactly a week accepted", err)
	}
}

func TestLoadDir_RejectsATrackTheCatalogWouldNotHold(t *testing.T) {
	long := strings.Repeat("a", 301)
	for name, track := range map[string]eval.Track{
		"title blank once trimmed":        {Title: "   ", Artist: "a"},
		"artist blank once canonicalised": {Title: "t", Artist: "\u3000"},
		"title over 300 bytes":            {Title: long, Artist: "a"},
		"artist over 300 bytes":           {Title: "t", Artist: long},
		"album over 300 bytes":            {Title: "t", Artist: "a", Album: long},
	} {
		t.Run(name, func(t *testing.T) {
			c := eval.Case{ID: "x", Class: "OK", Track: track, Candidates: []eval.Candidate{{URL: "u"}}}
			if err := validate(t, []eval.Case{c}); err == nil || !strings.Contains(err.Error(), "empty or over 300 bytes once the catalog trims it") {
				t.Fatalf("LoadDir = %v, want a catalog text error", err)
			}
		})
	}
	edge := eval.Case{ID: "x", Class: "OK", Track: eval.Track{Title: strings.Repeat("t", 300), Artist: " a ", Album: " "}, Candidates: []eval.Candidate{{URL: "u"}}}
	if err := validate(t, []eval.Case{edge}); err != nil {
		t.Fatalf("LoadDir = %v, want 300 bytes, a padded artist and a blank album accepted", err)
	}
}

func TestRun_RejectedSourceKeysKeepTheNewestTwentyFiveLikeTheCatalog(t *testing.T) {
	var urls []string
	for i := range 26 {
		urls = append(urls, fmt.Sprintf("https://www.youtube.com/watch?v=videoid%04d", i))
	}
	urls = append(urls, urls[0])
	kase := eval.Case{
		ID: "t", Class: "F7", ExcludeURLs: urls,
		Track:      eval.Track{Title: "Save Your Tears", Artist: "The Weeknd", Duration: 215},
		Candidates: []eval.Candidate{{Title: "Cooking Tutorial Episode 47", URL: "cook", Channel: "Cooking", Duration: 215}},
	}

	got := eval.Run(context.Background(), kase).Saved.RejectedKeys

	if len(got) != 25 || got[0] != "youtube:videoid0002" || got[24] != "youtube:videoid0000" {
		t.Errorf("saved rejected keys %v, want videoid0002..videoid0025 then videoid0000 again: a key pushed out of the 25 can be rejected again", got)
	}
}

func TestLoadDir_RejectsAnUnknownExpectedSavedProvenance(t *testing.T) {
	cases := []eval.Case{{
		ID: "x", Class: "OK", ExpectSaved: &eval.SavedExpectation{Provenance: "made_up"},
		Track:      eval.Track{Title: "t", Artist: "a"},
		Candidates: []eval.Candidate{{URL: "u"}},
	}}

	if err := validate(t, cases); err == nil || !strings.Contains(err.Error(), "unknown saved provenance") {
		t.Fatalf("LoadDir = %v, want an unknown saved provenance error", err)
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

			_, err := eval.LoadDir(dir)

			if err == nil || !strings.Contains(err.Error(), g.want) || !strings.Contains(err.Error(), `"shape"`) {
				t.Fatalf("LoadDir = %v, want a refusal naming case %q and %q", err, "shape", g.want)
			}
		})
	}
}

func TestRun_FlatCandidatesRunInProdTiersByKind(t *testing.T) {
	kase := eval.Case{
		ID: "t", Class: "OK",
		Track: eval.Track{Title: "Solitude", Artist: "Nova", Duration: 200, AuthoritativeDuration: 200},
		Candidates: []eval.Candidate{
			{Title: "Solitude", URL: "upload", Channel: "Nova - Topic", Duration: 200, ViewCount: 9000000},
			{Title: "Solitude", URL: "art", Duration: 200, Resolved: true, Kind: "art_track", Artists: []string{"Nova"}, Correct: true},
		},
	}

	out := eval.Run(context.Background(), kase)

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

func loadedCandidate(t *testing.T, candidate string) eval.Candidate {
	t.Helper()
	cases, err := eval.LoadDir(writeGolden(t, candidate))
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(cases) != 1 || len(cases[0].Candidates) != 1 {
		t.Fatalf("loaded %d cases, want one case with one candidate", len(cases))
	}
	return cases[0].Candidates[0]
}

func factsOf(c eval.Candidate) eval.Candidate {
	c.Observed = nil
	return c
}

func emitted(c ports.AudioCandidate) eval.Candidate {
	return eval.Candidate{
		Title: c.Title, URL: c.URL, Channel: c.Channel, Categories: c.Categories, Duration: c.Duration,
		ViewCount: c.ViewCount, Resolved: c.IsResolved(), Kind: string(c.Kind), Artists: c.Artists, Album: c.Album, Explicit: c.Explicit,
	}
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

		got := loadedCandidate(t, `{"observed":{"ytmsearch_song":{"VideoID":"abcdefghijk","Title":"Halo","Artists":["Nova"],"Album":"Glow","DurationSeconds":201,"Type":"MUSIC_VIDEO_TYPE_ATV","Explicit":true}}}`)

		if got, emits := factsOf(got), emitted(want[0]); !reflect.DeepEqual(got, emits) {
			t.Errorf("golden candidate = %+v, want what ytmsearch Find emits: %+v", got, emits)
		}
	})

	t.Run("ytmusic watch", func(t *testing.T) {
		watch := ytmsearch.Watch{VideoID: "abcdefghijk", Title: "Halo", Artists: []string{"Nova"}, Type: ytmsearch.VideoTypeOMV}
		want, err := ytmusic.NewSource(nil, fixedWatch(watch)).Find(ctx, pinReq)
		if err != nil || len(want) != 1 {
			t.Fatalf("ytmusic Find = %v, %v", want, err)
		}

		got := loadedCandidate(t, `{"observed":{"ytmusic_watch":{"VideoID":"abcdefghijk","Title":"Halo","Artists":["Nova"],"Type":"MUSIC_VIDEO_TYPE_OMV","Pinned":true}}}`)

		if got, emits := factsOf(got), emitted(want[0]); !reflect.DeepEqual(got, emits) {
			t.Errorf("golden candidate = %+v, want what ytmusic Find emits: %+v", got, emits)
		}
	})

	t.Run("ytdlp entry", func(t *testing.T) {
		line := `{"title":"Halo","duration":201.5,"webpage_url":"https://soundcloud.com/nova/halo","channel":"Nova","categories":["Music"],"view_count":7}`

		got := factsOf(loadedCandidate(t, `{"observed":{"ytdlp_entry":`+line+`}}`))

		want := emitted(ports.AudioCandidate{
			Title: "Halo", Duration: 201.5, URL: "https://soundcloud.com/nova/halo",
			Channel: "Nova", Categories: []string{"Music"}, ViewCount: 7, Kind: ports.KindUpload,
		})
		if !reflect.DeepEqual(got, want) {
			t.Errorf("golden candidate = %+v, want every yt-dlp entry field carried over as an unresolved upload: %+v", got, want)
		}
		if got.Resolved {
			t.Errorf("golden candidate resolved, want an unresolved upload")
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
			if _, err := eval.LoadDir(writeGolden(t, candidate)); err == nil || !strings.Contains(err.Error(), `"shape"`) {
				t.Fatalf("LoadDir = %v, want a refusal naming case %q", err, "shape")
			}
		})
	}
}

func capturedLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &logged
}

func auditedGrade(t *testing.T, logged *bytes.Buffer) string {
	t.Helper()
	for line := range strings.Lines(logged.String()) {
		if !strings.Contains(line, "msg=acquisition.audio_verdict") {
			continue
		}
		for field := range strings.FieldsSeq(line) {
			if grade, ok := strings.CutPrefix(field, "grade="); ok {
				return grade
			}
		}
	}
	t.Fatalf("no audio verdict logged in %q", logged.String())
	return ""
}

func TestRun_APinnedVideoIsGradedAsTheKindItsWatchReports(t *testing.T) {
	const url = "https://music.youtube.com/watch?v=abcdefghijk"
	watch := func(videoType ytmsearch.VideoType) *eval.Observed {
		return &eval.Observed{Watch: &eval.ObservedWatch{Watch: ytmsearch.Watch{VideoID: "abcdefghijk", Title: "Halo", Artists: []string{"Nova"}, Type: videoType}}}
	}
	pinned := func(cand eval.Candidate) eval.Case {
		cand.URL, cand.Correct, cand.ActualDuration = url, true, 200
		cand.AcoustID, cand.RecordingMBIDs = "halo-ac", []string{"halo"}
		if cand.Observed == nil {
			cand.Title, cand.Artists, cand.Duration = "Halo", []string{"Nova"}, 200
		}
		return eval.Case{
			ID: "watch", Class: "OK",
			Track: eval.Track{
				Title: "Halo", Artist: "Nova", CatalogDuration: 200, MBID: "halo",
				CatalogSources: []eval.CatalogSource{{Provider: string(ports.ProviderYouTube), ExternalID: "abcdefghijk"}},
			},
			Sources: []eval.Source{{Name: "ytmusic", Adapter: "ytmusic", Candidates: []eval.Candidate{cand}}},
		}
	}
	tests := []struct {
		name      string
		candidate eval.Candidate
		want      string
	}{
		{"an art track is an official ATV", eval.Candidate{Kind: "art_track", Resolved: true}, "A"},
		{"an upload is a fan UGC", eval.Candidate{Kind: "upload"}, "B"},
		{"an observed OMV watch is returned as observed", eval.Candidate{Observed: watch(ytmsearch.VideoTypeOMV)}, "A"},
		{"an observed UGC watch is returned as observed", eval.Candidate{Observed: watch(ytmsearch.VideoTypeUGC)}, "B"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logged := capturedLog(t)

			out := eval.Run(context.Background(), pinned(tt.candidate))

			if out.Stored != url {
				t.Fatalf("stored %q (%s), want the pinned video %q", out.Stored, out.Reason, url)
			}
			if got := auditedGrade(t, logged); got != tt.want {
				t.Errorf("grade %q, want %q for a fingerprint-matched %s", got, tt.want, tt.name)
			}
		})
	}
}

func TestEmbedded_TheCatalogLengthGoldenIsWhatProdsAdaptersEmit(t *testing.T) {
	kase := embeddedCase(t, "pl-catalog-length-wins-over-a-doubted-search-pick")

	var artTracks, uploads int
	for _, cand := range allCandidates(kase) {
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
	if out := eval.Run(context.Background(), kase); !out.Pass {
		t.Errorf("expected pass, got %q (err %q)", out.Reason, out.Err)
	}
}

var liveSearchGoldens = []string{"pipeline.json", "current_file.json", "verification.json", "art_track_length.json"}

const handBuiltNoteMarker = "hand-built because"

func TestEmbedded_LiveSearchCandidatesAreObserved(t *testing.T) {
	for _, name := range liveSearchGoldens {
		raw, err := os.ReadFile(filepath.Join("goldens", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		cases, err := eval.LoadDir(dir)
		if err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
		for _, kase := range cases {
			if strings.Contains(kase.Note, handBuiltNoteMarker) {
				continue
			}
			for _, cand := range allCandidates(kase) {
				if cand.Observed == nil && strings.HasPrefix(cand.URL, "http") {
					t.Errorf("%s: case %q: live-search candidate %q is hand-built; record the adapter's raw input under observed, or say %q in the case note", name, kase.ID, cand.URL, handBuiltNoteMarker)
				}
			}
		}
	}
}

func TestLoadDir_RefusesAResolvedFlagTheKindDenies(t *testing.T) {
	for name, cand := range map[string]eval.Candidate{
		"resolved upload":       {URL: "u", Resolved: true, Kind: "upload"},
		"resolved with no kind": {URL: "u", Resolved: true},
		"unresolved pin":        {URL: "u", Kind: "pinned_video"},
	} {
		t.Run(name, func(t *testing.T) {
			err := validate(t, []eval.Case{{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Candidates: []eval.Candidate{cand}}})
			if err == nil {
				t.Fatal("LoadDir accepted a resolved flag the kind denies")
			}
		})
	}
}

func TestRun_AnUnplayableCandidateIsNeverStored(t *testing.T) {
	kase := embeddedCase(t, "unplayable-soundcloud-previews-are-never-stored-the-full-upload-is")
	const fullUpload = "https://soundcloud.com/koolandthegang/get-down-on-it"

	t.Run("the full upload is stored beside the previews", func(t *testing.T) {
		out := eval.Run(context.Background(), kase)

		if !out.Pass || out.Stored != fullUpload {
			t.Fatalf("Run stored %q (pass=%v: %s), want the full upload %q", out.Stored, out.Pass, out.Reason, fullUpload)
		}
		if out.TopRanked != fullUpload || out.Attempts != 1 {
			t.Errorf("ranked %q and attempted %d candidates, want only the full upload ranked and attempted", out.TopRanked, out.Attempts)
		}
	})

	t.Run("a preview listed at the full length is rejected before any download", func(t *testing.T) {
		var previews []eval.Candidate
		for _, c := range allCandidates(kase) {
			if c.Unplayable == ports.UnplayablePreview {
				c.Duration, c.ActualDuration, c.Correct = kase.Track.CatalogDuration, 30, false
				previews = append(previews, c)
			}
		}
		if len(previews) == 0 {
			t.Fatal("the golden holds no candidate marked a preview")
		}
		kase.Sources = []eval.Source{{Name: "ytdlp", SearchSeconds: 3, Candidates: previews}}

		out := eval.Run(context.Background(), kase)

		if out.Stored != "" || out.Attempts != 0 {
			t.Errorf("stored %q after %d download attempts, want no download of a preview", out.Stored, out.Attempts)
		}
	})
}

func TestLoad_RefusesAnUnplayableReasonProdNeverSets(t *testing.T) {
	_, err := eval.LoadDir(writeGolden(t, `{"url":"https://soundcloud.com/a/b","unplayable":"paywall"}`))
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

			out := eval.Run(context.Background(), kase)

			if !out.Pass || out.Stored != "" {
				t.Errorf("pass=%v stored=%q reason=%q, want nothing stored: the stored source is excluded by its url", out.Pass, out.Stored, out.Reason)
			}
		})
	}
}

func TestLoadDir_AcceptsASourceURLForATrackWithNoStoredFile(t *testing.T) {
	cases := []eval.Case{{
		ID: "x", Class: "OK",
		Track:      eval.Track{Title: "t", Artist: "a", AudioSourceURL: "https://www.youtube.com/watch?v=7wBgcalM4c4"},
		Candidates: []eval.Candidate{{URL: "u"}},
	}}

	if err := validate(t, cases); err != nil {
		t.Fatalf("LoadDir = %v, want nil", err)
	}
}

func TestRun_RecordedObservationsGoThroughTheRealAdapters(t *testing.T) {
	const explicitURL = "https://music.youtube.com/watch?v=bcdefghijkl"
	const cleanURL = "https://music.youtube.com/watch?v=abcdefghijk"
	recorded := func(pin []eval.CatalogSource) eval.Case {
		return eval.Case{
			ID: "x", Class: "OK",
			Track: eval.Track{Title: "Halo", Artist: "Nova", CatalogDuration: 200, CatalogSources: pin},
			Sources: []eval.Source{
				{Name: "ytmsearch", ObservedSongs: []eval.ObservedSong{
					{VideoID: "zyxwvutsrqp", Title: "Halo (Live)", Artists: []string{"Nova"}, DurationSeconds: 200, Type: ytmsearch.VideoTypeOMV},
					{VideoID: "abcdefghijk", Title: "Halo", Artists: []string{"Nova"}, DurationSeconds: 200, Type: ytmsearch.VideoTypeATV},
					{VideoID: "bcdefghijkl", Title: "Halo", Artists: []string{"Nova"}, DurationSeconds: 200, Type: ytmsearch.VideoTypeATV, Explicit: true},
				}, Candidates: []eval.Candidate{
					{URL: cleanURL, ActualDuration: 200},
					{URL: explicitURL, ActualDuration: 200, Correct: true},
				}},
				{Name: "ytmusic", ObservedWatches: map[string]eval.ObservedWatchRecord{
					"abcdefghijk": {Title: "Halo", Artists: []string{"Nova"}, Type: ytmsearch.VideoTypeATV},
				}},
			},
		}
	}

	t.Run("ytmsearch stores the strong hit its own tie-break picks among the recorded songs", func(t *testing.T) {
		out := eval.Run(context.Background(), recorded(nil))

		if !out.Pass || out.Stored != explicitURL {
			t.Fatalf("Run stored %q (pass=%v: %s), want the explicit art track %q", out.Stored, out.Pass, out.Reason, explicitURL)
		}
	})

	watchesAlone := func(pin []eval.CatalogSource) eval.Case {
		kase := recorded(pin)
		kase.Track.MBID = "halo"
		kase.Sources = kase.Sources[1:]
		kase.Sources[0].Candidates = []eval.Candidate{{URL: cleanURL, ActualDuration: 200, AcoustID: "halo-ac", RecordingMBIDs: []string{"halo"}, Correct: true}}
		return kase
	}

	t.Run("ytmusic yields no candidate when the request pins no video", func(t *testing.T) {
		out := eval.Run(context.Background(), watchesAlone(nil))

		if out.Stored != "" || out.Attempts != 0 {
			t.Errorf("stored %q after %d attempts, want no candidate at all", out.Stored, out.Attempts)
		}
	})

	t.Run("ytmusic emits the recorded watch of the pinned video as an art track", func(t *testing.T) {
		logged := capturedLog(t)

		out := eval.Run(context.Background(), watchesAlone([]eval.CatalogSource{{Provider: string(ports.ProviderYouTube), ExternalID: "abcdefghijk"}}))

		if out.Stored != cleanURL {
			t.Fatalf("stored %q (%s), want the recorded watch of the pinned video at %q", out.Stored, out.Reason, cleanURL)
		}
		if got := auditedGrade(t, logged); got != "A" {
			t.Errorf("grade %q, want A: a fingerprint-matched art track is official, a pinned video of unknown type is not", got)
		}
	})
}

func TestLoadDir_RefusesARecordedSourceThatBreaksItsAdapter(t *testing.T) {
	song := []eval.ObservedSong{{VideoID: "abcdefghijk", Title: "Halo", Type: ytmsearch.VideoTypeATV}}
	watch := map[string]eval.ObservedWatchRecord{"abcdefghijk": {Title: "Halo", Type: ytmsearch.VideoTypeATV}}
	facts := []eval.Candidate{{URL: "u"}}
	tests := []struct {
		name string
		src  eval.Source
		want string
	}{
		{"songs on a source that is not ytmsearch", eval.Source{Name: "ytdlp", ObservedSongs: song, Candidates: facts}, "records ytmsearch songs"},
		{"watches on a source that is not ytmusic", eval.Source{Name: "ytmsearch", ObservedWatches: watch, Candidates: facts}, "records ytmusic watches"},
		{"songs beside a named adapter", eval.Source{Name: "ytmsearch", Adapter: "ytmusic", ObservedSongs: song, Candidates: facts}, "keep one"},
		{"a hand-set title beside the recording", eval.Source{Name: "ytmsearch", ObservedSongs: song, Candidates: []eval.Candidate{{URL: "u", Title: "Halo"}}}, "may carry only a url and download facts"},
		{"an observed candidate beside the songs", eval.Source{Name: "ytmsearch", ObservedSongs: song, Candidates: []eval.Candidate{{Observed: &eval.Observed{Entry: &ytdlp.Entry{WebpageURL: "https://soundcloud.com/a/b"}}}}}, "may carry only a url and download facts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(t, []eval.Case{{ID: "x", Class: "F1", Track: eval.Track{Title: "t", Artist: "a"}, Sources: []eval.Source{tt.src}}})

			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadDir = %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

func TestRun_ItsOnlyLoveStoresTheClassicVersionWhenTheOriginalIsNowhere(t *testing.T) {
	kase := embeddedCase(t, "rw-its-only-love-original-nowhere-takes-classic-version")

	out := eval.Run(context.Background(), kase)

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

	out := eval.Run(context.Background(), kase)

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

func TestRun_FailsOnADifferingSavedTitleAlbumYearOrMBID(t *testing.T) {
	kase := embeddedCase(t, "rw-its-only-love-original-nowhere-takes-classic-version")
	saved := eval.Run(context.Background(), kase).Saved
	if saved.Title == "" || saved.Album == "" || saved.Year == 0 || saved.CatalogMBID == "" {
		t.Fatalf("saved %+v, want a title, album, year and mbid to compare against", saved)
	}
	tests := []struct {
		name string
		want eval.SavedExpectation
	}{
		{"title", eval.SavedExpectation{Title: "other"}},
		{"album", eval.SavedExpectation{Album: "other"}},
		{"year", eval.SavedExpectation{Year: 1984}},
		{"mbid", eval.SavedExpectation{MBID: "other"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kase.ExpectSaved = &tt.want
			if out := eval.Run(context.Background(), kase); out.Pass || !strings.Contains(out.Reason, "want saved "+tt.name) {
				t.Fatalf("pass=%v reason %q, want a failure for a differing %s", out.Pass, out.Reason, tt.name)
			}
		})
	}
	kase.ExpectSaved = &eval.SavedExpectation{Title: saved.Title, Album: saved.Album, Year: saved.Year, MBID: saved.CatalogMBID}
	if out := eval.Run(context.Background(), kase); !out.Pass {
		t.Fatalf("expected a match, got %q", out.Reason)
	}
}
