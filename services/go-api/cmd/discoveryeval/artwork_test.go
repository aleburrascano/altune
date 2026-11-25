package main

import (
	"context"
	"slices"
	"testing"
)

func TestLoadLibraryArtists_ReadsTheStoredArtistsInOrderOrShuffled(t *testing.T) {
	pool, _, artist := seedLibraryTrack(t)

	for _, random := range []bool{false, true} {
		artists, err := loadLibraryArtists(context.Background(), pool, 0, random)
		if err != nil || !slices.Contains(artists, artist) {
			t.Fatalf("loadLibraryArtists(random=%v) = %d artists, %v; want the seeded artist", random, len(artists), err)
		}
	}
}
