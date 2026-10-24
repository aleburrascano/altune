package trackers

import (
	"os"
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

func TestParseHub_MapsArtistNameToTheSheetIDInItsHyperlink(t *testing.T) {
	index, err := parseHub(readFixture(t, "hub_artists.json"))
	if err != nil {
		t.Fatalf("parseHub: %v", err)
	}
	if got := index["ken carson"]; got != kenSheetID {
		t.Errorf("index[ken carson] = %q, want %q", got, kenSheetID)
	}
}

func TestParseHub_SkipsLinksThatAreNotSheets(t *testing.T) {
	index, err := parseHub(readFixture(t, "hub_artists.json"))
	if err != nil {
		t.Fatalf("parseHub: %v", err)
	}
	for name, id := range index {
		if name == "" || id == "" {
			t.Errorf("empty entry %q -> %q", name, id)
		}
	}
	if id, ok := index["asap rocky"]; ok {
		t.Errorf("a non-Sheets tracker got an entry: %q", id)
	}
	if len(index) != 4 {
		t.Errorf("index has %d entries, want 4 (50 Cent, Ariana Grande, Baby Keem, Ken Carson): %v", len(index), index)
	}
}

func TestParseHub_RejectsMalformedJSON(t *testing.T) {
	if _, err := parseHub([]byte("{")); err == nil {
		t.Error("parseHub accepted malformed JSON")
	}
}
