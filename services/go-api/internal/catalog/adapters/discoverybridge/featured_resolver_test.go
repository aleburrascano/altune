package discoverybridge

import (
	"altune/go-api/internal/shared"
	"context"
	"strings"
	"testing"
)

type stubFeaturedResolver struct {
	feats []shared.FeaturedArtist
}

func (s stubFeaturedResolver) Resolve(context.Context, string, string) ([]shared.FeaturedArtist, error) {
	return s.feats, nil
}

// Provider credits feed the backfill, which persists without the add-track
// validation, so a credit over the catalog field caps is dropped rather than
// failing the whole track; real credits pass through.
func TestFeaturedResolver_SkipsOversizedCredits(t *testing.T) {
	inner := stubFeaturedResolver{feats: []shared.FeaturedArtist{
		{Name: "Michael Jackson", MBID: "f27ec8db-af05-4f36-916e-3d57f91ecf5e"},
		{Name: strings.Repeat("n", 301)},
		{Name: "Bogus", MBID: strings.Repeat("a", 37)},
		{Name: "Deezer Only", DeezerID: 259},
	}}

	got, err := NewFeaturedResolver(inner).Resolve(context.Background(), "Artist", "Title")
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(got) != 2 || got[0].Name != "Michael Jackson" || got[1].Name != "Deezer Only" {
		t.Errorf("resolved = %+v, want only the two in-cap credits", got)
	}
}
