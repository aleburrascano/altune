package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func chronoAlbum(title string, year int) ConsensusAlbum {
	album := res(domain.ResultKindAlbum, title, "Artist", domain.ProviderDeezer, nil)
	album.Year = year
	return ConsensusAlbum{
		Album:  album,
		Status: ConsensusConfirmed,
	}
}

func TestSortChronological_NewestFirstUnknownLast(t *testing.T) {
	in := []ConsensusAlbum{
		chronoAlbum("Old", 2018),
		chronoAlbum("Unknown", 0),
		chronoAlbum("Newest", 2024),
		chronoAlbum("Mid", 2021),
		chronoAlbum("Older", 2017),
	}

	sortChronological(in)

	got := make([]string, len(in))
	for i, a := range in {
		got[i] = a.Album.Title
	}
	want := []string{"Newest", "Mid", "Old", "Older", "Unknown"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}
