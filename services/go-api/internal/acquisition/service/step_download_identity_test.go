package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"testing"
)

func TestLogAudioVerdict_CarriesTheAcoustIDResults(t *testing.T) {
	buf := captureJSONLog(t)
	results := []ports.AcoustIDResult{
		{Score: 0.97, Recordings: []ports.LinkedRecording{{MBID: "mbid-a"}, {MBID: "mbid-b"}}},
		{Score: 0.41},
	}

	logAudioVerdict(context.Background(), logTestDownloadContext(), logTestCandidate(), AudioVerdict{}, results)

	logged := findLogRecord(t, buf, "acquisition.audio_verdict")["results"].([]any)
	if len(logged) != 2 {
		t.Fatalf("logged %d results, want 2", len(logged))
	}
	first := logged[0].(map[string]any)
	if first["score"] != 0.97 {
		t.Fatalf("first score = %v, want 0.97", first["score"])
	}
	recordings := first["recordings"].([]any)
	if len(recordings) != 2 || recordings[0] != "mbid-a" || recordings[1] != "mbid-b" {
		t.Fatalf("first recordings = %v, want [mbid-a mbid-b]", recordings)
	}
	if second := logged[1].(map[string]any); second["score"] != 0.41 {
		t.Fatalf("second score = %v, want 0.41", second["score"])
	}
}

func TestLogAudioVerdict_CapsTheResultsAndTheirRecordings(t *testing.T) {
	buf := captureJSONLog(t)
	recordings := make([]ports.LinkedRecording, maxLoggedRecordings+3)
	results := make([]ports.AcoustIDResult, maxLoggedResults+3)
	for i := range results {
		results[i] = ports.AcoustIDResult{Score: 0.5, Recordings: recordings}
	}

	logAudioVerdict(context.Background(), logTestDownloadContext(), logTestCandidate(), AudioVerdict{}, results)

	logged := findLogRecord(t, buf, "acquisition.audio_verdict")["results"].([]any)
	if len(logged) != maxLoggedResults {
		t.Fatalf("logged %d results, want the cap %d", len(logged), maxLoggedResults)
	}
	if got := len(logged[0].(map[string]any)["recordings"].([]any)); got != maxLoggedRecordings {
		t.Fatalf("logged %d recordings, want the cap %d", got, maxLoggedRecordings)
	}
}
