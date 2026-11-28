package service_test

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/ports"
	"altune/go-api/internal/library/internal/service"
	"altune/go-api/internal/library/librarytest"
	"context"
	"strings"
	"testing"
)

type fakeCredits struct {
	credits []ports.FeaturedArtistCredit
	err     error
}

func (f fakeCredits) FeaturedArtistCredits(context.Context, string, string) ([]ports.FeaturedArtistCredit, error) {
	return f.credits, f.err
}

func resolverOverCredits(_ *testing.T, feats []domain.FeaturedArtist, err error) ports.FeaturedArtistResolver {
	credits := make([]ports.FeaturedArtistCredit, 0, len(feats))
	for _, f := range feats {
		credits = append(credits, ports.FeaturedArtistCredit{Name: f.Name, MBID: f.MBID, DeezerID: f.DeezerID})
	}
	return service.NewFeaturedArtistResolver(fakeCredits{credits: credits, err: err})
}

func TestFeaturedArtistResolver_Contract(t *testing.T) {
	librarytest.FeaturedResolverContract(t, resolverOverCredits)
}

func TestFeaturedArtistResolver_SkipsOversizedCredits(t *testing.T) {
	resolver := service.NewFeaturedArtistResolver(fakeCredits{credits: []ports.FeaturedArtistCredit{
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
	resolver := service.NewFeaturedArtistResolver(fakeCredits{credits: []ports.FeaturedArtistCredit{{Name: "   ", DeezerID: 7}, {Name: "Guest"}}})

	got, err := resolver.Resolve(context.Background(), "Artist", "Title")
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "Guest" {
		t.Errorf("resolved = %+v, want only the named credit", got)
	}
}

func TestFeaturedArtistResolver_KeepsEachCreditsIdentityAsAFeaturedRole(t *testing.T) {
	resolver := service.NewFeaturedArtistResolver(fakeCredits{credits: []ports.FeaturedArtistCredit{
		{Name: "  Guest  ", MBID: " f27ec8db-af05-4f36-916e-3d57f91ecf5e ", DeezerID: 259},
	}})

	got, err := resolver.Resolve(context.Background(), "Artist", "Title")
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	want, _ := domain.NewFeaturedArtist("Guest", "f27ec8db-af05-4f36-916e-3d57f91ecf5e", 259)
	if len(got) != 1 || got[0] != want {
		t.Errorf("resolved = %+v, want [%+v]", got, want)
	}
}
