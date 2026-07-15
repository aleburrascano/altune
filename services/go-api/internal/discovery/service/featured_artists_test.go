package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestMergeFeaturedArtists(t *testing.T) {
	t.Run("mb only", func(t *testing.T) {
		mb := []domain.FeaturedArtist{{Name: "SZA", MBID: "m1"}}
		got := MergeFeaturedArtists(mb, nil)
		if len(got) != 1 || got[0].MBID != "m1" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("deezer only", func(t *testing.T) {
		dz := []domain.FeaturedArtist{{Name: "SZA", DeezerID: 7}}
		got := MergeFeaturedArtists(nil, dz)
		if len(got) != 1 || got[0].DeezerID != 7 {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("both agree — same name merges ids, MB order kept", func(t *testing.T) {
		mb := []domain.FeaturedArtist{{Name: "SZA", MBID: "m1"}}
		dz := []domain.FeaturedArtist{{Name: "sza", DeezerID: 7}}
		got := MergeFeaturedArtists(mb, dz)
		if len(got) != 1 {
			t.Fatalf("expected 1 merged, got %d (%+v)", len(got), got)
		}
		if got[0].MBID != "m1" || got[0].DeezerID != 7 {
			t.Errorf("merged entry = %+v, want MBID m1 + DeezerID 7", got[0])
		}
	})

	t.Run("deezer fills gap MB missed", func(t *testing.T) {
		mb := []domain.FeaturedArtist{{Name: "SZA", MBID: "m1"}}
		dz := []domain.FeaturedArtist{{Name: "SZA", DeezerID: 7}, {Name: "Doja", DeezerID: 8}}
		got := MergeFeaturedArtists(mb, dz)
		if len(got) != 2 {
			t.Fatalf("expected 2, got %d (%+v)", len(got), got)
		}
		if got[1].Name != "Doja" || got[1].DeezerID != 8 {
			t.Errorf("gap-fill entry = %+v, want Doja / 8", got[1])
		}
	})

	t.Run("neither", func(t *testing.T) {
		if got := MergeFeaturedArtists(nil, nil); len(got) != 0 {
			t.Errorf("expected empty, got %+v", got)
		}
	})
}
