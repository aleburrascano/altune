package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sampleResults() []trackResult {
	return []trackResult{
		{TrackID: "t-1", Artist: "A", Title: "One", Verdict: verdictConfirmed},
		{
			TrackID: "t-2", Artist: "B", Title: "Two", Verdict: verdictMismatch,
			Identity:   boundIdentity{Status: statusBound, MBRecordingID: "rec-bound"},
			Candidates: []candidate{{MBRecordingID: "rec-other", Artists: []string{"Someone"}, Score: 0.95}},
		},
		{TrackID: "t-3", Artist: "C", Title: "Three", Verdict: verdictFailed, Error: "stream gone"},
	}
}

func TestWriteReport_IsJSONWithCountsAndEveryTrack(t *testing.T) {
	var out bytes.Buffer
	report := buildReport(sampleResults(), 0.85, time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC))

	if err := writeReport(&out, report); err != nil {
		t.Fatalf("writeReport: %v", err)
	}

	var decoded struct {
		Total   int            `json:"total"`
		Counts  map[string]int `json:"counts"`
		Results []struct {
			TrackID    string `json:"track_id"`
			Verdict    string `json:"verdict"`
			Candidates []struct {
				MBRecordingID string `json:"mb_recording_id"`
			} `json:"acoustid_candidates"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("report is not JSON: %v", err)
	}
	if decoded.Total != 3 || decoded.Counts["confirmed"] != 1 || decoded.Counts["mismatch"] != 1 || decoded.Counts["failed"] != 1 {
		t.Errorf("total/counts = %d %v, want 3 with one each", decoded.Total, decoded.Counts)
	}
	if len(decoded.Results) != 3 || decoded.Results[1].Candidates[0].MBRecordingID != "rec-other" {
		t.Errorf("results = %+v, want all tracks with the mismatch's AcoustID candidates", decoded.Results)
	}
}

func TestWriteSummary_ListsMismatchesAndFailuresButNotConfirmed(t *testing.T) {
	var out bytes.Buffer

	writeSummary(&out, buildReport(sampleResults(), 0.85, time.Now()), "/x/r.json")

	text := out.String()
	for _, want := range []string{"verified 3 tracks", "confirmed:", "mismatch", "t-2", "rec-bound", "rec-other", "stream gone", "report: /x/r.json"} {
		if !strings.Contains(text, want) {
			t.Errorf("summary missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "t-1") {
		t.Errorf("summary lists a confirmed track:\n%s", text)
	}
}
