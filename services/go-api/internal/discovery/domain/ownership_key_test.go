package domain_test

import (
	"altune/go-api/internal/discovery/domain"
	"testing"
)

func TestOwnershipKey_FoldsTitleAndArtistSpellings(t *testing.T) {
	if domain.OwnershipKey("Alive", "Pearl Jam") != domain.OwnershipKey("alive", "PEARL JAM") {
		t.Fatal("case-only spellings of one title and artist must share a key")
	}
	if domain.OwnershipKey("Alive", "Pearl Jam") == domain.OwnershipKey("Black", "Pearl Jam") {
		t.Fatal("different titles must not share a key")
	}
}

func TestOwnershipKey_DecorationKeepsTheSongAndVersionsSplitIt(t *testing.T) {
	same := []string{
		"Song (Remastered 2011)", "Song [Deluxe Edition]", "Song (Explicit)",
		"Song (Radio Edit)", "Song - Remastered 2011", "Song - Radio Edit", "Song (Single Version)",
		"Song (Album Version)",
	}
	for _, title := range same {
		if domain.OwnershipKey(title, "X") != domain.OwnershipKey("Song", "X") {
			t.Errorf("%q must share a key with the plain title", title)
		}
	}
	different := []string{
		"Song (Live)", "Song (Live at Wembley)", "Song - Remix", "Song (Acoustic)", "Song (Slowed)",
		"Song (A Cappella)", "Song [Instrumental]", "Song - Live", "Song (Sped Up)",
		"Song - Part 2", "Song - II. Adagio", "Song (Pt. 2)", "Song (Interlude)",
		"Song - Clean Bandit Remix", "Song (Explicit Remix)", "Song - feat. Y - Live",
		"Song (feat. Y)", "Song - feat. Y", "Song (feat. Y Remix)", "Song (feat. Y Acoustic)",
		"Song (feat. Y Live Version)", "Song - feat. Y Live",
		"Song (Piano Version)", "Song (Spanish Version)", "Song (With Orchestra)", "Song (with Y)",
	}
	for _, title := range different {
		if domain.OwnershipKey(title, "X") == domain.OwnershipKey("Song", "X") {
			t.Errorf("%q must not share a key with the plain title", title)
		}
	}
	if domain.OwnershipKey("Song (Live)", "X") == domain.OwnershipKey("Song (Remix)", "X") {
		t.Error("live and remix versions must not share a key")
	}
}
