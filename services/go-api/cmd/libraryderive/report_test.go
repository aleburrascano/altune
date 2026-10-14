package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFindingOf_AGroupedCorrectionCarriesItsGroupIntoTheFixJSON(t *testing.T) {
	old, value := "Now 14", "Simple Pleasures"
	d := difference{Field: "album", Mode: modeCorrect, Old: &old, New: &value, Group: "album-move:t1"}
	found := findingOf(boundTrack{libraryTrack: libraryTrack{ID: "t1", UserID: "u1", Version: 3}}, d)
	encoded, err := json.Marshal(found)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"group":"album-move:t1"`) || found.Category != "album_correct" {
		t.Fatalf("category %s json %s", found.Category, encoded)
	}
}

func TestOtherMasterFindings_AnAlbumOrTitleDifferenceIsReportedNotFixedWhileTheMasterChanges(t *testing.T) {
	stored := boundTrack{libraryTrack: libraryTrack{ID: "t1", UserID: "u1", Title: "Careless Whispers", Artist: "Wham!", Album: "Greatest Hits", ISRC: "GBAYE8400001", FileSeconds: 300}}
	master := deezerTrack{ISRC: "GBAYE8400002", AlbumTitle: "Make It Big", DurationSeconds: 300}
	found := otherMasterFindings(stored, anchor{Title: "Careless Whisper", Artist: "Wham!"}, pick{Group: makeItBig()}, master)
	byKind := byCategory(found)
	if byKind["album_differs"].Category == "" || byKind["title_differs"].Category == "" || byKind[categoryISRCOtherMaster].LengthPlan == nil {
		t.Fatalf("got %+v", found)
	}
	for _, f := range found {
		if f.Fix != nil {
			t.Errorf("%s carries a fix", f.Category)
		}
	}
}
