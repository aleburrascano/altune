package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func boundRushDecision(t *testing.T) decided {
	t.Helper()
	got, err := bindWithArtistFirst(t, rushTomSawyer(), rushHeld(), musicBrainzKnowing(),
		rushSource(sourceDeezer, rushSong("d1", nil)), rushSource(sourceApple, rushSong("a1", nil)))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRelengthPlan_FixesTheReplacedISRCInTheShapeLibraryrelengthReads(t *testing.T) {
	refused := boundRushDecision(t)
	notRefused := boundRushDecision(t)
	notRefused.Found.Evidence.Replaced.Refusal = reasonISRCNotFound
	unbound := decided{Track: rushTomSawyer(), Found: identity{Status: statusUnbound, Reason: reasonRefusedPrefix + "title"}}

	path := filepath.Join(t.TempDir(), "plan.json")
	if err := writeRelengthPlan(path, relengthPlan([]decided{refused, notRefused, unbound}, false)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Aborted  bool `json:"aborted"`
		Findings []struct {
			TrackID    string         `json:"track_id"`
			Artist     string         `json:"artist"`
			Title      string         `json:"title"`
			LengthPlan map[string]any `json:"length_plan"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if file.Aborted || len(file.Findings) != 1 {
		t.Fatalf("findings = %+v", file.Findings)
	}
	finding := file.Findings[0]
	want := map[string]any{
		"action": "fix_isrc", "user_id": "user-1", "version": 3.0, "old_isrc": rushLiveISRC, "new_isrc": rushStudioISRC,
		"file_seconds": 274.0, "catalog_seconds": 274.0,
	}
	if finding.TrackID != "track-rush" || finding.Artist != "Rush" || finding.Title != "Tom Sawyer" || len(finding.LengthPlan) != len(want) {
		t.Fatalf("finding = %+v", finding)
	}
	for key, value := range want {
		if finding.LengthPlan[key] != value {
			t.Errorf("length_plan.%s = %v, want %v", key, finding.LengthPlan[key], value)
		}
	}
}

func TestRelengthPlan_NeedsDeezerAndAppleNotMusicBrainzEchoingOne(t *testing.T) {
	got, err := bindWithArtistFirst(t, rushTomSawyer(), rushHeld(), musicBrainzKnowing(rushSong("mb-1", nil)),
		rushSource(sourceDeezer, rushSong("d1", nil)), rushSource(sourceApple))
	if err != nil {
		t.Fatal(err)
	}
	if got.Found.Status != statusBound || got.Found.ISRC != rushStudioISRC || got.Found.MBRecordingID != "mb-1" {
		t.Fatalf("got %+v", got.Found)
	}
	if plan := relengthPlan([]decided{got}, false); len(plan.Findings) != 0 {
		t.Errorf("plan = %+v, want no fix_isrc from deezer and musicbrainz alone", plan.Findings)
	}
}

func TestRelengthPlan_MarksAnAbortedRunPartial(t *testing.T) {
	if plan := relengthPlan(nil, true); !plan.Aborted || plan.Findings == nil {
		t.Fatalf("plan = %+v", plan)
	}
}
