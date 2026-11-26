package libraryfromsearch_test

import (
	"altune/go-api/internal/app/bridges/libraryfromsearch"
	"altune/go-api/internal/library"
	"altune/go-api/internal/search"
	"context"
	"errors"
	"reflect"
	"testing"
)

const deezerTrackID = "3135556"

type fakeDeezerFeatured struct {
	feats []search.FeaturedArtist
	err   error
}

func (f fakeDeezerFeatured) ResolveID(context.Context, search.ResultKind, string, string) (string, error) {
	return deezerTrackID, f.err
}

func (f fakeDeezerFeatured) LookupTrackFeatured(_ context.Context, trackID string) ([]search.FeaturedArtist, error) {
	if trackID != deezerTrackID {
		return nil, nil
	}
	return f.feats, nil
}

func resolverOverCatalogCredits(t *testing.T, feats []library.FeaturedArtist, err error) library.FeaturedArtistResolver {
	upstream := make([]search.FeaturedArtist, 0, len(feats))
	for _, f := range feats {
		upstream = append(upstream, search.FeaturedArtist{Name: f.Name, MBID: f.MBID, DeezerID: f.DeezerID})
	}
	discovered, resolverErr := search.NewFeaturedArtistResolver(search.NoMBFeatured{}, fakeDeezerFeatured{feats: upstream, err: err})
	if resolverErr != nil {
		t.Fatal(resolverErr)
	}
	return library.NewFeaturedArtistResolver(libraryfromsearch.NewFeaturedArtistCredits(discovered))
}

type stubSource struct {
	feats  []search.FeaturedArtist
	err    error
	artist string
	title  string
}

func (s *stubSource) Resolve(_ context.Context, artist, title string) ([]search.FeaturedArtist, error) {
	s.artist, s.title = artist, title
	return s.feats, s.err
}

func TestFeaturedArtistCredits_CarriesEachArtistsIdentityInOrder(t *testing.T) {
	source := &stubSource{feats: []search.FeaturedArtist{
		{Name: "Guest", MBID: "f27ec8db-af05-4f36-916e-3d57f91ecf5e", DeezerID: 259, Role: "featured"},
		{Name: "  Unnamed  "},
	}}

	got, err := libraryfromsearch.NewFeaturedArtistCredits(source).FeaturedArtistCredits(context.Background(), "Artist", "Song")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []library.FeaturedArtistCredit{
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

	got, err := libraryfromsearch.NewFeaturedArtistCredits(&stubSource{err: cause}).FeaturedArtistCredits(context.Background(), "Artist", "Song")

	if !errors.Is(err, cause) || got != nil {
		t.Errorf("credits = %v, err = %v; want none and %v", got, err, cause)
	}
}

func liveDiscoveryProviderNames() []string {
	var names []string
	for p := search.ProviderUnknown + 1; p <= search.ProviderSpotify; p++ {
		if _, err := search.ParseProviderName(p.String()); err == nil {
			names = append(names, p.String())
		}
	}
	return names
}

func TestKnownCatalogProviders_AcceptEveryLiveDiscoveryProvider(t *testing.T) {
	accepted := map[string]bool{}
	for _, name := range library.KnownCatalogProviders() {
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
