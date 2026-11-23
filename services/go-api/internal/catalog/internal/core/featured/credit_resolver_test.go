package featured_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/catalog/catalogtest"
	"context"
	"strings"
	"testing"
)

type fakeCredits struct {
	credits []catalog.FeaturedArtistCredit
	err     error
}

func (f fakeCredits) FeaturedArtistCredits(context.Context, string, string) ([]catalog.FeaturedArtistCredit, error) {
	return f.credits, f.err
}

func resolverOverCredits(_ *testing.T, feats []catalog.FeaturedArtist, err error) catalog.FeaturedArtistResolver {
	credits := make([]catalog.FeaturedArtistCredit, 0, len(feats))
	for _, f := range feats {
		credits = append(credits, catalog.FeaturedArtistCredit{Name: f.Name, MBID: f.MBID, DeezerID: f.DeezerID})
	}
	return catalog.NewFeaturedArtistResolver(fakeCredits{credits: credits, err: err})
}

func TestFeaturedArtistResolver_Contract(t *testing.T) {
	catalogtest.FeaturedResolverContract(t, resolverOverCredits)
}

func TestFeaturedArtistResolver_SkipsOversizedCredits(t *testing.T) {
	resolver := catalog.NewFeaturedArtistResolver(fakeCredits{credits: []catalog.FeaturedArtistCredit{
		{Name: "Michael Jackson", MBID: "f27ec8db-af05-4f36-916e-3d57f91ecf5e"},
		{Name: strings.Repeat("n", 301)},
		{Name: "Bogus", MBID: strings.Repeat("a", 37)},
		{Name: "Deezer Only", DeezerID: 259},
	}})

	got, err := resolver.Resolve(context.Background(), "Artist", "Title")
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(got) != 2 || got[0].Name != "Michael Jackson" || got[1].Name != "Deezer Only" {
		t.Errorf("resolved = %+v, want only the two in-cap credits", got)
	}
}

func TestFeaturedArtistResolver_SkipsBlankNamedCredits(t *testing.T) {
	resolver := catalog.NewFeaturedArtistResolver(fakeCredits{credits: []catalog.FeaturedArtistCredit{{Name: "   ", DeezerID: 7}, {Name: "Guest"}}})

	got, err := resolver.Resolve(context.Background(), "Artist", "Title")
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "Guest" {
		t.Errorf("resolved = %+v, want only the named credit", got)
	}
}

func TestFeaturedArtistResolver_KeepsEachCreditsIdentityAsAFeaturedRole(t *testing.T) {
	resolver := catalog.NewFeaturedArtistResolver(fakeCredits{credits: []catalog.FeaturedArtistCredit{
		{Name: "  Guest  ", MBID: " f27ec8db-af05-4f36-916e-3d57f91ecf5e ", DeezerID: 259},
	}})

	got, err := resolver.Resolve(context.Background(), "Artist", "Title")
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	want, _ := catalog.NewFeaturedArtist("Guest", "f27ec8db-af05-4f36-916e-3d57f91ecf5e", 259)
	if len(got) != 1 || got[0] != want {
		t.Errorf("resolved = %+v, want [%+v]", got, want)
	}
}
