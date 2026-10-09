package domain

import "testing"

func TestOwnershipKey_FoldsTitleAndArtistSpellings(t *testing.T) {
	if OwnershipKey("Alive", "Pearl Jam") != OwnershipKey("alive", "PEARL JAM") {
		t.Fatal("case-only spellings of one title and artist must share a key")
	}
	if OwnershipKey("Alive", "Pearl Jam") == OwnershipKey("Black", "Pearl Jam") {
		t.Fatal("different titles must not share a key")
	}
}
