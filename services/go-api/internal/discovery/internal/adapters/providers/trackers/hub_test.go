package trackers_test

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
)

const kenSheetID = "1OARID98xCqRaBr8gyQCvI3aD4jKQDGgtedyRaiP_pyo"

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestHub_MapsArtistNameToTheSheetIDInItsHyperlink(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), readFixture(t, "ken_carson_unreleased.json"))
	songs, err := srv.client(nil).ArtistSongs(context.Background(), "Ken Carson")
	if err != nil || len(songs) == 0 || srv.artistCalls.Load() == 0 {
		t.Errorf("ArtistSongs = %d songs, err %v, %d reads of sheet %s; want Ken Carson's sheet read", len(songs), err, srv.artistCalls.Load(), kenSheetID)
	}
}

func TestHub_SkipsLinksThatAreNotSheets(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), nil)
	names, err := srv.client(nil).Artists(context.Background())
	if err != nil {
		t.Fatalf("Artists: %v", err)
	}
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			t.Errorf("empty artist name in %q", names)
		}
	}
	if slices.Contains(names, "asap rocky") {
		t.Errorf("a non-Sheets tracker got an entry: %q", names)
	}
	if len(names) != 4 {
		t.Errorf("hub has %d artists, want 4 (50 Cent, Ariana Grande, Baby Keem, Ken Carson): %q", len(names), names)
	}
}
