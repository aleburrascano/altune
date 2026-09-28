package main

import (
	"altune/go-api/internal/acquisition/service/eval"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testDump = `{
  "track": {
    "id": "3b0fe67b-1111-2222-3333-444455556666",
    "title": "Drinking in L.A.",
    "artist": "Bran Van 3000",
    "album": "Glee",
    "duration_seconds": 236,
    "isrc": "CAA509814003",
    "acquisition_status": "failed",
    "failure_reason": "no correct candidate: all 2 candidates rejected (2 fingerprint)",
    "audio_source_url": ""
  },
  "logs": [
    {"time":"2026-09-01T00:00:00Z","level":"INFO","msg":"candidate_evaluated","track_id":"3b0fe67b","source":"youtube","candidate_title":"Drinking in L.A. (Who Mix?)","candidate_channel":"Bran Van 3000 - Topic","candidate_duration":307,"candidate_views":100,"identity_score":8,"metadata_rank":0.2,"qualifier_distance":1,"is_topic":true,"artist_match":true,"feature_match":true,"track_artist":"Bran Van 3000"},
    {"time":"2026-09-01T00:00:01Z","level":"INFO","msg":"candidate_evaluated","track_id":"3b0fe67b","source":"soundcloud","candidate_title":"Drinking in LA","candidate_channel":"randomuploader","candidate_duration":236,"candidate_views":50,"identity_score":9,"metadata_rank":0.5,"qualifier_distance":0,"is_topic":false,"artist_match":false,"feature_match":true,"track_artist":"Bran Van 3000"},
    {"time":"2026-09-01T00:00:02Z","level":"INFO","msg":"acquisition.rejection_summary","track_id":"3b0fe67b","summary":"all 2 candidates rejected (2 fingerprint)"}
  ]
}`

func TestRunCapture_PrintsCaseThatValidateCasesAccepts(t *testing.T) {
	var out bytes.Buffer
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)

	code := runCapture([]string{"-tier", "staging"}, strings.NewReader(testDump), &out, now)
	if code != 0 {
		t.Fatalf("runCapture exit code = %d, stderr not captured; want 0", code)
	}

	var kase eval.Case
	if err := json.Unmarshal(out.Bytes(), &kase); err != nil {
		t.Fatalf("output is not a single json object: %v", err)
	}
	if kase.Class != "RW" {
		t.Fatalf("class = %q, want RW", kase.Class)
	}
	if !strings.Contains(kase.Note, "all 2 candidates rejected (2 fingerprint)") {
		t.Fatalf("note = %q, want it to record the rejection stage summary", kase.Note)
	}
	if !strings.Contains(kase.Note, "captured 2026-09-28 from staging") {
		t.Fatalf("note = %q, want the capture disclaimer", kase.Note)
	}

	gotCandidates := 0
	for _, src := range kase.Sources {
		gotCandidates += len(src.Candidates)
	}
	if gotCandidates != 2 {
		t.Fatalf("candidate count = %d, want one per candidate_evaluated line (2)", gotCandidates)
	}

	dir := t.TempDir()
	suite := map[string]any{"cases": []eval.Case{kase}}
	raw, err := json.Marshal(suite)
	if err != nil {
		t.Fatalf("marshal suite: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "captured.json"), raw, 0o600); err != nil {
		t.Fatalf("write golden: %v", err)
	}

	if _, err := eval.LoadDir(dir); err != nil {
		t.Fatalf("validateCases (via LoadDir) rejected the captured case: %v", err)
	}
}

func TestRunCapture_ExitsTwoOnUnparseableInput(t *testing.T) {
	var out bytes.Buffer
	code := runCapture(nil, strings.NewReader("not json"), &out, time.Now())
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 for unparseable input", code)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing written on failure", out.String())
	}
}

func TestRunCapture_ExitsTwoWhenNoCandidateLogsPresent(t *testing.T) {
	var out bytes.Buffer
	dump := `{"track":{"id":"t1","title":"T","artist":"A","duration_seconds":100},"logs":[]}`
	code := runCapture(nil, strings.NewReader(dump), &out, time.Now())
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 when the dump has no candidate_evaluated lines", code)
	}
}

func TestRun_DefaultBehaviourUnaffectedByCaptureSubcommand(t *testing.T) {
	if err := run("", "", false); err != nil {
		t.Fatalf("default run() (no subcommand) still failed: %v", err)
	}
}

func probeCapture(t *testing.T, args []string, dump string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code := runCapture(args, strings.NewReader(dump), &out, time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC))
	return code, out.String()
}

func probeDecodeAndValidate(t *testing.T, out string) eval.Case {
	t.Helper()
	var kase eval.Case
	if err := json.Unmarshal([]byte(out), &kase); err != nil {
		t.Fatalf("output %q is not one json case: %v", out, err)
	}
	dir := t.TempDir()
	raw, err := json.Marshal(map[string]any{"cases": []eval.Case{kase}})
	if err != nil {
		t.Fatalf("marshal suite: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "captured.json"), raw, 0o600); err != nil {
		t.Fatalf("write golden: %v", err)
	}
	if _, err := eval.LoadDir(dir); err != nil {
		t.Fatalf("validateCases rejected the captured case: %v\ncase: %s", err, out)
	}
	return kase
}

func probeCandidateCount(kase eval.Case) int {
	n := len(kase.Candidates)
	for _, src := range kase.Sources {
		n += len(src.Candidates)
	}
	return n
}

const probeTrack = `{"id":"3b0fe67b-1111-2222-3333-444455556666","title":"Drinking in L.A.","artist":"Bran Van 3000","album":"Glee","duration_seconds":236,"isrc":"CAA509814003","acquisition_status":"failed","failure_reason":"x","audio_source_url":""}`

func probeCandLine(source, title string) string {
	return `{"msg":"candidate_evaluated","source":"` + source + `","candidate_title":"` + title + `","candidate_channel":"ch","candidate_duration":236,"candidate_views":10}`
}

func TestRunCapture_RepeatedCandidateLinesStillProduceAValidCase(t *testing.T) {
	dump := `{"track":` + probeTrack + `,"logs":[` +
		probeCandLine("youtube", "Drinking in L.A.") + `,` +
		probeCandLine("youtube", "Drinking in L.A.") + `,` +
		probeCandLine("soundcloud", "Drinking in L.A.") + `]}`
	code, out := probeCapture(t, []string{"-tier", "staging"}, dump)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	kase := probeDecodeAndValidate(t, out)
	if got := probeCandidateCount(kase); got != 3 {
		t.Fatalf("candidate count = %d, want 3 (one per candidate_evaluated line)", got)
	}
}

func TestRunCapture_IgnoresLogLinesThatAreNotCandidatesOrRejections(t *testing.T) {
	dump := `{"track":` + probeTrack + `,"logs":[` +
		`{"msg":"http request","path":"/v1/tracks","status":200},` +
		probeCandLine("youtube", "A") + `,` +
		`{"level":"WARN","msg":"download failed","source":"youtube","candidate_title":"A"},` +
		probeCandLine("youtube", "B") + `]}`
	code, out := probeCapture(t, []string{"-tier", "staging"}, dump)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	kase := probeDecodeAndValidate(t, out)
	if got := probeCandidateCount(kase); got != 2 {
		t.Fatalf("candidate count = %d, want 2 (only candidate_evaluated lines count)", got)
	}
}

func TestRunCapture_CandidateMissingOptionalFieldsIsOmittedNotRejected(t *testing.T) {
	dump := `{"track":` + probeTrack + `,"logs":[` +
		`{"msg":"candidate_evaluated","source":"youtube","candidate_title":"Drinking in L.A."}]}`
	code, out := probeCapture(t, []string{"-tier", "staging"}, dump)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	kase := probeDecodeAndValidate(t, out)
	if got := probeCandidateCount(kase); got != 1 {
		t.Fatalf("candidate count = %d, want 1", got)
	}
	for _, src := range kase.Sources {
		for _, c := range src.Candidates {
			if c.Duration != 0 || c.ViewCount != 0 || c.Channel != "" {
				t.Fatalf("candidate = %+v, want absent duration/views/channel omitted", c)
			}
		}
	}
}

func TestRunCapture_SuccessfulAcquisitionStillCarriesReviewNote(t *testing.T) {
	dump := `{"track":` + probeTrack + `,"logs":[` + probeCandLine("youtube", "Drinking in L.A.") + `]}`
	code, out := probeCapture(t, []string{"-tier", "live"}, dump)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a dump with no rejection_summary", code)
	}
	kase := probeDecodeAndValidate(t, out)
	if !strings.Contains(kase.Note, "captured 2026-09-28 from live") || !strings.Contains(kase.Note, "review before committing") {
		t.Fatalf("note = %q, want \"captured 2026-09-28 from live; review before committing\"", kase.Note)
	}
	if kase.Class != "RW" {
		t.Fatalf("class = %q, want RW", kase.Class)
	}
}

func TestRunCapture_CarriesTheTrackRowIntoTheCase(t *testing.T) {
	code, out := probeCapture(t, []string{"-tier", "staging"}, testDump)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	kase := probeDecodeAndValidate(t, out)
	tr := kase.Track
	if tr.Title != "Drinking in L.A." || tr.Artist != "Bran Van 3000" || tr.Album != "Glee" || tr.Duration != 236 || tr.ISRC != "CAA509814003" {
		t.Fatalf("track = %+v, want title/artist/album/duration/isrc from the dump row", tr)
	}
}

func TestRunCapture_WrongShapeOrTruncatedInputExitsTwoWithNoOutput(t *testing.T) {
	inputs := map[string]string{
		"empty":         "",
		"array":         `[]`,
		"track string":  `{"track":"x","logs":[]}`,
		"logs string":   `{"track":` + probeTrack + `,"logs":"x"}`,
		"truncated":     `{"track":` + probeTrack + `,"logs":[` + probeCandLine("youtube", "A"),
		"string number": `{"track":` + probeTrack + `,"logs":[{"msg":"candidate_evaluated","source":"youtube","candidate_title":"A","candidate_duration":"307"}]}`,
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			code, out := probeCapture(t, nil, in)
			if code != 2 {
				t.Fatalf("exit code = %d for %q, want 2", code, in)
			}
			if out != "" {
				t.Fatalf("stdout = %q, want nothing on failure", out)
			}
		})
	}
}

func TestRunCapture_NonLatinTitleWithoutTrackIDStillGetsAValidCaseID(t *testing.T) {
	dump := `{"track":{"id":"","title":"東京","artist":"椎名林檎","duration_seconds":200},"logs":[` + probeCandLine("youtube", "東京") + `]}`
	code, out := probeCapture(t, []string{"-tier", "staging"}, dump)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	probeDecodeAndValidate(t, out)

	kase := probeDecodeAndValidate(t, out)
	if kase.ID == "" || kase.ID == "captured-" || !strings.HasPrefix(kase.ID, "captured-") {
		t.Fatalf("id = %q, want a non-empty id starting with \"captured-\" (a slug or hash fallback), not the bare prefix", kase.ID)
	}
}

func TestRunCapture_SameDumpTwiceGivesIdenticalCase(t *testing.T) {
	_, first := probeCapture(t, []string{"-tier", "staging"}, testDump)
	_, second := probeCapture(t, []string{"-tier", "staging"}, testDump)
	if first != second {
		t.Fatalf("capture is not deterministic:\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestRunCapture_ManyCandidatesAllCapturedAndValid(t *testing.T) {
	lines := make([]string, 0, 500)
	for i := range 500 {
		src := "youtube"
		if i%2 == 1 {
			src = "soundcloud"
		}
		lines = append(lines, probeCandLine(src, "Drinking in L.A. take "+strings.Repeat("x", i%7)))
	}
	dump := `{"track":` + probeTrack + `,"logs":[` + strings.Join(lines, ",") + `]}`
	code, out := probeCapture(t, []string{"-tier", "staging"}, dump)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	kase := probeDecodeAndValidate(t, out)
	if got := probeCandidateCount(kase); got != 500 {
		t.Fatalf("candidate count = %d, want 500", got)
	}
}

func TestRunCapture_PrintsExactlyOneJSONObject(t *testing.T) {
	_, out := probeCapture(t, []string{"-tier", "staging"}, testDump)
	dec := json.NewDecoder(strings.NewReader(out))
	var first map[string]any
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("first decode: %v", err)
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		t.Fatalf("stdout has a second json value %v after the case, want exactly one", extra)
	}
}

func TestRunCapture_NeverPrintsACaseThatValidateCasesRejects(t *testing.T) {
	dumps := map[string]string{
		"no source on candidate": `{"track":` + probeTrack + `,"logs":[{"msg":"candidate_evaluated","candidate_title":"A"}]}`,
		"no title on candidate":  `{"track":` + probeTrack + `,"logs":[{"msg":"candidate_evaluated","source":"youtube"}]}`,
		"track without artist":   `{"track":{"id":"t1","title":"T","artist":""},"logs":[` + probeCandLine("youtube", "A") + `]}`,
	}
	for name, dump := range dumps {
		t.Run(name, func(t *testing.T) {
			code, out := probeCapture(t, []string{"-tier", "staging"}, dump)
			if code != 0 {
				if out != "" {
					t.Fatalf("exit code = %d but stdout = %q, want nothing on failure", code, out)
				}
				return
			}
			probeDecodeAndValidate(t, out)
		})
	}
}
