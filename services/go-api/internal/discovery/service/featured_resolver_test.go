package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

type fakeMBFeat struct {
	results []domain.SearchResult
	err     error
}

func (f fakeMBFeat) SearchStructured(_ context.Context, _, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return f.results, f.err
}

type fakeDeezerFeat struct {
	id    string
	feats []domain.FeaturedArtist
	err   error
}

func (f fakeDeezerFeat) ResolveID(_ context.Context, _ domain.ResultKind, _, _ string) (string, error) {
	return f.id, f.err
}
func (f fakeDeezerFeat) LookupTrackFeatured(_ context.Context, _ string) ([]domain.FeaturedArtist, error) {
	return f.feats, nil
}

func mbResultWithFeatured(feats ...domain.FeaturedArtist) domain.SearchResult {
	return domain.SearchResult{
		Kind:   domain.ResultKindTrack,
		Title:  "Song",
		Extras: map[string]any{"featured_artists": domain.FeaturedArtistsToExtras(feats)},
	}
}

func TestFeaturedArtistResolver_Resolve(t *testing.T) {
	ctx := context.Background()

	t.Run("merges mb + deezer, mb-primary", func(t *testing.T) {
		mb := fakeMBFeat{results: []domain.SearchResult{mbResultWithFeatured(
			domain.FeaturedArtist{Name: "SZA", MBID: "m1", Role: domain.RoleFeatured},
		)}}
		dz := fakeDeezerFeat{id: "42", feats: []domain.FeaturedArtist{
			{Name: "SZA", DeezerID: 7, Role: domain.RoleFeatured},
			{Name: "Doja", DeezerID: 8, Role: domain.RoleFeatured},
		}}
		got, _ := NewFeaturedArtistResolver(mb, dz).Resolve(ctx, "Artist", "Song")
		if len(got) != 2 {
			t.Fatalf("expected 2, got %d (%+v)", len(got), got)
		}
		if got[0].MBID != "m1" || got[0].DeezerID != 7 {
			t.Errorf("merged[0] = %+v, want MBID m1 + DeezerID 7", got[0])
		}
		if got[1].Name != "Doja" {
			t.Errorf("gap-fill = %+v, want Doja", got[1])
		}
	})

	t.Run("mb error degrades to deezer only", func(t *testing.T) {
		mb := fakeMBFeat{err: errors.New("mb down")}
		dz := fakeDeezerFeat{id: "1", feats: []domain.FeaturedArtist{{Name: "Guest", DeezerID: 9}}}
		got, _ := NewFeaturedArtistResolver(mb, dz).Resolve(ctx, "A", "B")
		if len(got) != 1 || got[0].Name != "Guest" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("deezer unresolved degrades to mb only", func(t *testing.T) {
		mb := fakeMBFeat{results: []domain.SearchResult{mbResultWithFeatured(
			domain.FeaturedArtist{Name: "Only MB", MBID: "m"},
		)}}
		dz := fakeDeezerFeat{id: ""}
		got, _ := NewFeaturedArtistResolver(mb, dz).Resolve(ctx, "A", "B")
		if len(got) != 1 || got[0].Name != "Only MB" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("neither yields empty", func(t *testing.T) {
		got, _ := NewFeaturedArtistResolver(fakeMBFeat{}, fakeDeezerFeat{}).Resolve(ctx, "A", "B")
		if len(got) != 0 {
			t.Fatalf("got %+v", got)
		}
	})
}
