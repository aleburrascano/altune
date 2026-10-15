package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
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

func TestCheckpointTo_LeavesAWholePartialReportAndNoTempFile(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "report.json")

	checkpointTo(reportPath, 0.85, io.Discard)(sampleResults()[:2])

	raw, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var decoded struct {
		Partial bool `json:"partial"`
		Total   int  `json:"total"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil || !decoded.Partial || decoded.Total != 2 {
		t.Errorf("report = %s (%v), want a partial report of 2 tracks", raw, err)
	}
	if _, err := os.Stat(reportPath + ".pending"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("pending file still exists: %v", err)
	}
}

func TestSaveJSON_RemovesThePendingFileWhenEncodingFails(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "report.json")

	err := saveJSON(reportPath, map[string]any{"unencodable": make(chan int)})

	if err == nil {
		t.Fatal("saveJSON returned no error for an unencodable value")
	}
	for _, leftover := range []string{reportPath, reportPath + ".pending"} {
		if _, statErr := os.Stat(leftover); !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("%s exists after a failed save: %v", leftover, statErr)
		}
	}
}

func TestPublishCalibration_LeavesTheWholeReportAndNoTempFile(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "calibration.json")

	err := publishCalibration(calibrationReport{Same: distribution{Count: 1}}, reportPath, io.Discard)
	if err != nil {
		t.Fatalf("publishCalibration: %v", err)
	}
	if _, err := os.Stat(reportPath); err != nil {
		t.Errorf("report missing: %v", err)
	}
	if _, err := os.Stat(reportPath + ".pending"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("pending file still exists: %v", err)
	}
}

func TestCheckpointTo_SaysSoWhenTheReportCannotBeWritten(t *testing.T) {
	var progress bytes.Buffer

	checkpointTo(filepath.Join(t.TempDir(), "missing", "report.json"), 0.85, &progress)(sampleResults())

	if !strings.Contains(progress.String(), "checkpoint of 3 tracks failed") {
		t.Errorf("progress = %q, want the failed checkpoint named", progress.String())
	}
}

func TestWriteSummary_ListsWrongAudioWithTheCatalogAndAcoustIDEvidence(t *testing.T) {
	var out bytes.Buffer
	wrong := trackResult{
		TrackID: "t-9", Artist: "Travis Scott", Title: "outside", Verdict: verdictWrongAudio,
		Identity:   boundIdentity{Status: statusBound, ISRC: "USSM11607554"},
		Candidates: []candidate{{MBRecordingID: "rec-trap", Title: "beibs in the trap", Artists: []string{"Travis Scott"}, Score: 0.96}},
		Preview:    &previewEvidence{Alignment: &alignmentEvidence{BitErrorRate: 0.47, OffsetSeconds: 12}},
	}

	writeSummary(&out, buildReport([]trackResult{wrong}, 0.85, time.Now()), "/x/r.json")

	for _, want := range []string{"wrong_audio", "t-9", "beibs in the trap", "0.47", "USSM11607554"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary missing %q:\n%s", want, out.String())
		}
	}
}
