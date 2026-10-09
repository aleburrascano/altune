package catalogtest

import (
	"altune/go-api/internal/catalog/domain"
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestFeaturedResolver_ZeroValueResolvesNothing(t *testing.T) {
	got, err := FeaturedResolver{}.Resolve(context.Background(), "Artist", "Song")
	if got != nil || err != nil {
		t.Fatalf("zero resolver = %v, %v; want nil, nil", got, err)
	}
}

func TestFeaturedResolver_ReturnsArtistsForExactTitle(t *testing.T) {
	want := []domain.FeaturedArtist{{Name: "Guest"}}
	resolver := FeaturedResolver{ByTitle: map[string][]domain.FeaturedArtist{"Song": want}}

	got, err := resolver.Resolve(context.Background(), "Artist", "Song")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve(Song) = %v, %v; want %v, nil", got, err, want)
	}
	lookAlike, err := resolver.Resolve(context.Background(), "Artist", "song")
	if lookAlike != nil || err != nil {
		t.Fatalf("Resolve(song) = %v, %v; want nil, nil", lookAlike, err)
	}
}

func TestFeaturedResolver_ReturnsSetError(t *testing.T) {
	wantErr := errors.New("provider down")
	resolver := FeaturedResolver{
		ByTitle: map[string][]domain.FeaturedArtist{"Song": {{Name: "Guest"}}},
		Err:     wantErr,
	}

	got, err := resolver.Resolve(context.Background(), "Artist", "Song")
	if !errors.Is(err, wantErr) || got != nil {
		t.Fatalf("Resolve = %v, %v; want nil, %v", got, err, wantErr)
	}
}

func TestFeaturedResolver_ChangingByTitleChangesNextResult(t *testing.T) {
	resolver := FeaturedResolver{ByTitle: map[string][]domain.FeaturedArtist{"Song": {{Name: "First"}}}}
	first, _ := resolver.Resolve(context.Background(), "Artist", "Song")

	resolver.ByTitle["Song"] = []domain.FeaturedArtist{{Name: "Second"}}
	second, _ := resolver.Resolve(context.Background(), "Artist", "Song")

	if first[0].Name != "First" || second[0].Name != "Second" {
		t.Fatalf("results = %v then %v; want First then Second", first, second)
	}
}
