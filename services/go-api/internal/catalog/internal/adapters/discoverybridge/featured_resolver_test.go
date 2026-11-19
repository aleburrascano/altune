package discoverybridge_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/discovery"
	"context"
	"strings"
	"testing"
)

const deezerTrackID = "3135556"

type fakeDeezerFeatured struct {
	feats []discovery.FeaturedArtist
	err   error
}

func (f fakeDeezerFeatured) ResolveID(context.Context, discovery.ResultKind, string, string) (string, error) {
	return deezerTrackID, f.err
}

func (f fakeDeezerFeatured) LookupTrackFeatured(_ context.Context, trackID string) ([]discovery.FeaturedArtist, error) {
	if trackID != deezerTrackID {
		return nil, nil
	}
	return f.feats, nil
}

func resolverOverDeezer(feats []discovery.FeaturedArtist, err error) *catalog.FeaturedResolver {
	return catalog.NewFeaturedResolver(discovery.NewFeaturedArtistResolver(nil, fakeDeezerFeatured{feats: feats, err: err}))
}

func resolverOverCatalogCredits(_ *testing.T, feats []catalog.FeaturedArtist, err error) catalog.FeaturedArtistResolver {
	upstream := make([]discovery.FeaturedArtist, 0, len(feats))
	for _, f := range feats {
		upstream = append(upstream, discovery.FeaturedArtist{Name: f.Name, MBID: f.MBID, DeezerID: f.DeezerID})
	}
	return resolverOverDeezer(upstream, err)
}

func TestFeaturedResolver_SkipsOversizedCredits(t *testing.T) {
	resolver := resolverOverDeezer([]discovery.FeaturedArtist{
		{Name: "Michael Jackson", MBID: "f27ec8db-af05-4f36-916e-3d57f91ecf5e"},
		{Name: strings.Repeat("n", 301)},
		{Name: "Bogus", MBID: strings.Repeat("a", 37)},
		{Name: "Deezer Only", DeezerID: 259},
	}, nil)

	got, err := resolver.Resolve(context.Background(), "Artist", "Title")
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(got) != 2 || got[0].Name != "Michael Jackson" || got[1].Name != "Deezer Only" {
		t.Errorf("resolved = %+v, want only the two in-cap credits", got)
	}
}

func TestFeaturedResolver_SkipsBlankNamedCredits(t *testing.T) {
	resolver := resolverOverDeezer([]discovery.FeaturedArtist{{Name: "   ", DeezerID: 7}, {Name: "Guest"}}, nil)

	got, err := resolver.Resolve(context.Background(), "Artist", "Title")
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "Guest" {
		t.Errorf("resolved = %+v, want only the named credit", got)
	}
}

func liveDiscoveryProviderNames() []string {
	var names []string
	for p := discovery.ProviderUnknown + 1; p <= discovery.ProviderSpotify; p++ {
		if _, err := discovery.ParseProviderName(p.String()); err == nil {
			names = append(names, p.String())
		}
	}
	return names
}

func TestKnownCatalogProviders_AcceptEveryLiveDiscoveryProvider(t *testing.T) {
	accepted := map[string]bool{}
	for _, name := range catalog.KnownCatalogProviders() {
		accepted[name] = true
	}
	live := liveDiscoveryProviderNames()
	if len(live) == 0 {
		t.Fatal("discovery parses none of its own provider names")
	}
	for _, name := range live {
		if !accepted[name] {
			t.Errorf("discovery provider %q is rejected by catalog source validation", name)
		}
	}
}
