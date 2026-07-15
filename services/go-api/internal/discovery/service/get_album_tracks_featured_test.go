package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

type fakeTrackFeatured struct {
	byID map[string][]domain.FeaturedArtist
	err  error
}

func (f fakeTrackFeatured) LookupTrackFeatured(_ context.Context, id string) ([]domain.FeaturedArtist, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byID[id], nil
}

func deezerTrackFeat(id, title string) domain.SearchResult {
	return domain.NewProviderResult(domain.ResultKindTrack, title, "", "",
		domain.SourceRef{Provider: domain.ProviderDeezer, ExternalID: id}, nil)
}

func TestGetAlbumTracks_enrichFeatured(t *testing.T) {
	ctx := context.Background()

	t.Run("stamps featured onto deezer tracks", func(t *testing.T) {
		svc := NewGetAlbumTracksService(nil, WithTrackFeatured(fakeTrackFeatured{
			byID: map[string][]domain.FeaturedArtist{
				"1": {{Name: "Destroy Lonely", DeezerID: 99, Role: domain.RoleFeatured}},
			},
		}))
		results := []domain.SearchResult{deezerTrackFeat("1", "Singapore"), deezerTrackFeat("2", "Lose It")}
		svc.enrichFeatured(ctx, results)

		raw, ok := results[0].Extras["featured_artists"].([]map[string]any)
		if !ok || len(raw) != 1 || raw[0]["name"] != "Destroy Lonely" {
			t.Fatalf("track 0 featured = %v", results[0].Extras["featured_artists"])
		}
		if _, present := results[1].Extras["featured_artists"]; present {
			t.Errorf("track 1 should have no featured, got %v", results[1].Extras["featured_artists"])
		}
	})

	t.Run("lookup error degrades to no features", func(t *testing.T) {
		svc := NewGetAlbumTracksService(nil, WithTrackFeatured(fakeTrackFeatured{err: errors.New("rate limited")}))
		results := []domain.SearchResult{deezerTrackFeat("1", "X")}
		svc.enrichFeatured(ctx, results)
		if _, present := results[0].Extras["featured_artists"]; present {
			t.Errorf("expected no featured on error, got %v", results[0].Extras["featured_artists"])
		}
	})

	t.Run("no lookup configured is a no-op", func(t *testing.T) {
		svc := NewGetAlbumTracksService(nil)
		results := []domain.SearchResult{deezerTrackFeat("1", "X")}
		svc.enrichFeatured(ctx, results) // must not panic
	})
}
