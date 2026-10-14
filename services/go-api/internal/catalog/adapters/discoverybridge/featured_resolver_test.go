package discoverybridge

import (
	"altune/go-api/internal/catalog/catalogtest"
	catalogdomain "altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	discoverydomain "altune/go-api/internal/discovery/domain"
	"context"
	"strings"
	"testing"
)

type stubFeaturedResolver struct {
	feats []discoverydomain.FeaturedArtist
	err   error
}

func (s stubFeaturedResolver) Resolve(context.Context, string, string) ([]discoverydomain.FeaturedArtist, error) {
	return s.feats, s.err
}

func TestFeaturedResolver_Contract(t *testing.T) {
	catalogtest.FeaturedResolverContract(t, func(_ *testing.T, feats []catalogdomain.FeaturedArtist, err error) ports.FeaturedArtistResolver {
		upstream := make([]discoverydomain.FeaturedArtist, 0, len(feats))
		for _, f := range feats {
			upstream = append(upstream, discoverydomain.FeaturedArtist{Name: f.Name, MBID: f.MBID, DeezerID: f.DeezerID})
		}
		return &FeaturedResolver{inner: stubFeaturedResolver{feats: upstream, err: err}}
	})
}

func TestFeaturedResolver_SkipsOversizedCredits(t *testing.T) {
	inner := stubFeaturedResolver{feats: []discoverydomain.FeaturedArtist{
		{Name: "Michael Jackson", MBID: "f27ec8db-af05-4f36-916e-3d57f91ecf5e"},
		{Name: strings.Repeat("n", 301)},
		{Name: "Bogus", MBID: strings.Repeat("a", 37)},
		{Name: "Deezer Only", DeezerID: 259},
	}}

	got, err := (&FeaturedResolver{inner: inner}).Resolve(context.Background(), "Artist", "Title")
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(got) != 2 || got[0].Name != "Michael Jackson" || got[1].Name != "Deezer Only" {
		t.Errorf("resolved = %+v, want only the two in-cap credits", got)
	}
}

func TestKnownCatalogProviders_MatchDiscoveryProviderNames(t *testing.T) {
	known := map[string]bool{}
	for _, name := range catalogdomain.KnownCatalogProviders() {
		if _, err := discoverydomain.ParseProviderName(name); err != nil {
			t.Errorf("catalog accepts provider %q that discovery does not know", name)
		}
		known[name] = true
	}
	for p := discoverydomain.ProviderUnknown + 1; p <= discoverydomain.ProviderSpotify; p++ {
		if !known[p.String()] {
			t.Errorf("discovery provider %q is rejected by catalog source validation", p.String())
		}
	}
}
