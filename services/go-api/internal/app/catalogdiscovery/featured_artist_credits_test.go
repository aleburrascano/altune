package catalogdiscovery_test

import (
	"altune/go-api/internal/app/catalogdiscovery"
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/discovery"
	"context"
	"errors"
	"reflect"
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

func resolverOverCatalogCredits(_ *testing.T, feats []catalog.FeaturedArtist, err error) catalog.FeaturedArtistResolver {
	upstream := make([]discovery.FeaturedArtist, 0, len(feats))
	for _, f := range feats {
		upstream = append(upstream, discovery.FeaturedArtist{Name: f.Name, MBID: f.MBID, DeezerID: f.DeezerID})
	}
	discovered := discovery.NewFeaturedArtistResolver(nil, fakeDeezerFeatured{feats: upstream, err: err})
	return catalog.NewFeaturedArtistResolver(catalogdiscovery.NewFeaturedArtistCredits(discovered))
}

type stubSource struct {
	feats  []discovery.FeaturedArtist
	err    error
	artist string
	title  string
}

func (s *stubSource) Resolve(_ context.Context, artist, title string) ([]discovery.FeaturedArtist, error) {
	s.artist, s.title = artist, title
	return s.feats, s.err
}

func TestFeaturedArtistCredits_CarriesEachArtistsIdentityInOrder(t *testing.T) {
	source := &stubSource{feats: []discovery.FeaturedArtist{
		{Name: "Guest", MBID: "f27ec8db-af05-4f36-916e-3d57f91ecf5e", DeezerID: 259, Role: "featured"},
		{Name: "  Unnamed  "},
	}}

	got, err := catalogdiscovery.NewFeaturedArtistCredits(source).FeaturedArtistCredits(context.Background(), "Artist", "Song")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []catalog.FeaturedArtistCredit{
		{Name: "Guest", MBID: "f27ec8db-af05-4f36-916e-3d57f91ecf5e", DeezerID: 259},
		{Name: "  Unnamed  "},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("credits = %+v, want %+v", got, want)
	}
	if source.artist != "Artist" || source.title != "Song" {
		t.Errorf("asked discovery for %q/%q, want Artist/Song", source.artist, source.title)
	}
}

func TestFeaturedArtistCredits_ReturnsDiscoverysError(t *testing.T) {
	cause := errors.New("provider down")

	got, err := catalogdiscovery.NewFeaturedArtistCredits(&stubSource{err: cause}).FeaturedArtistCredits(context.Background(), "Artist", "Song")

	if !errors.Is(err, cause) || got != nil {
		t.Errorf("credits = %v, err = %v; want none and %v", got, err, cause)
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
