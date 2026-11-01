package unreleased

import (
	"altune/go-api/internal/discovery/domain"
	"slices"
	"testing"
)

func geniusPage(id int, title string) domain.UnreleasedSong {
	return domain.UnreleasedSong{GeniusID: id, Title: title}
}

func TestNonTrackPagesAreDroppedAndCounted(t *testing.T) {
	genius := sourceResult{used: true, complete: true, songs: []domain.UnreleasedSong{
		geniusPage(1, "Miss Possessive Tour Setlist (2024)"),
		geniusPage(2, "End Credits"),
		geniusPage(3, "Tour Dates"),
		geniusPage(4, "Setlist"),
	}}
	trackers := sourceResult{used: true, complete: true, songs: []domain.UnreleasedSong{trackerSong("Setlist [V1]", "s1")}}

	got, err := combineSources(genius, trackers)
	if err != nil {
		t.Fatalf("combineSources: %v", err)
	}

	want := []string{"End Credits", "Setlist"}
	if !slices.Equal(titlesOf(got.songs), want) {
		t.Errorf("titles = %v, want %v", titlesOf(got.songs), want)
	}
	if got.nonSongDropped != 2 {
		t.Errorf("nonSongDropped = %d, want 2", got.nonSongDropped)
	}
	if len(got.songs) != 2 || got.songs[1].TrackerKey == "" || len(got.songs[1].AudioLinks) != 1 {
		t.Errorf("Setlist joined to its tracker file should keep the pixeldrain link, got %+v", got.songs)
	}
}
