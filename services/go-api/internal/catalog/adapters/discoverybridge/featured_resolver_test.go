package discoverybridge

import (
	"context"
	"errors"
	"strings"
	"testing"

	discoverydomain "altune/go-api/internal/discovery/domain"
)

type stubFeaturedResolver struct {
	feats []discoverydomain.FeaturedArtist
	err   error
}

func (s stubFeaturedResolver) Resolve(context.Context, string, string) ([]discoverydomain.FeaturedArtist, error) {
	return s.feats, s.err
}

func TestFeaturedResolver_EmptyInnerGivesNoResults(t *testing.T) {
	inner := stubFeaturedResolver{feats: []discoverydomain.FeaturedArtist{}}

	got, err := NewFeaturedResolver(inner).Resolve(context.Background(), "Artist", "Title")
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("resolved = %+v, want none", got)
	}
}

func TestFeaturedResolver_MapsOneArtist(t *testing.T) {
	const mbid = "f27ec8db-af05-4f36-916e-3d57f91ecf5e"
	inner := stubFeaturedResolver{feats: []discoverydomain.FeaturedArtist{
		{Name: "Michael Jackson", MBID: mbid, DeezerID: 259},
	}}

	got, err := NewFeaturedResolver(inner).Resolve(context.Background(), "Artist", "Title")
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "Michael Jackson" || got[0].MBID != mbid || got[0].DeezerID != 259 {
		t.Errorf("resolved = %+v, want Michael Jackson with mbid %s and deezer id 259", got, mbid)
	}
}

func TestFeaturedResolver_DropsEntryWithoutNameOrIDs(t *testing.T) {
	inner := stubFeaturedResolver{feats: []discoverydomain.FeaturedArtist{{Role: "feat"}}}

	got, err := NewFeaturedResolver(inner).Resolve(context.Background(), "Artist", "Title")
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("resolved = %+v, want the empty entry dropped", got)
	}
}

func TestFeaturedResolver_ReturnsInnerError(t *testing.T) {
	boom := errors.New("boom")
	inner := stubFeaturedResolver{err: boom}

	got, err := NewFeaturedResolver(inner).Resolve(context.Background(), "Artist", "Title")
	if !errors.Is(err, boom) {
		t.Fatalf("Resolve error = %v, want %v", err, boom)
	}
	if got != nil {
		t.Errorf("resolved = %+v, want nil on error", got)
	}
}

func TestFeaturedResolver_SkipsOversizedCredits(t *testing.T) {
	inner := stubFeaturedResolver{feats: []discoverydomain.FeaturedArtist{
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
