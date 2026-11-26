package artistpage_test

import (
	"altune/go-api/internal/search"
	"context"
	"slices"
	"sync"
	"testing"
)

type artistContentFuncs struct {
	topTracks func(ctx context.Context, provider search.ProviderName, id string) ([]search.SearchResult, error)
	albums    func(ctx context.Context, provider search.ProviderName, id string) ([]search.SearchResult, error)
}

func (p artistContentFuncs) GetArtistTopTracks(ctx context.Context, provider search.ProviderName, id string) ([]search.SearchResult, error) {
	if p.topTracks == nil {
		return nil, nil
	}
	return p.topTracks(ctx, provider, id)
}

func (p artistContentFuncs) GetArtistAlbums(ctx context.Context, provider search.ProviderName, id string) ([]search.SearchResult, error) {
	if p.albums == nil {
		return nil, nil
	}
	return p.albums(ctx, provider, id)
}

func topTrackLookups(t *testing.T, store search.IdentityStore, known map[search.ProviderName]string) map[search.ProviderName][]string {
	t.Helper()
	var mu sync.Mutex
	asked := map[search.ProviderName][]string{}
	providers := map[search.ProviderName]search.ArtistContentProvider{}
	for _, name := range []search.ProviderName{search.ProviderDeezer, search.ProviderSpotify, search.ProviderAppleMusic} {
		providers[name] = artistContentFuncs{topTracks: func(_ context.Context, provider search.ProviderName, id string) ([]search.SearchResult, error) {
			mu.Lock()
			defer mu.Unlock()
			asked[provider] = append(asked[provider], id)
			return nil, nil
		}}
	}
	var opts []search.ArtistContentOption
	if store != nil {
		opts = append(opts, search.WithContentIdentityStore(store))
	}
	svc := search.NewGetArtistContentService(providers, opts...)
	if _, err := svc.GetTopTracks(t.Context(), search.ProviderDeezer, "deezer-che", known, 10); err != nil {
		t.Fatalf("GetTopTracks: %v", err)
	}
	return asked
}

func assertAsked(t *testing.T, asked map[search.ProviderName][]string, want map[search.ProviderName]string) {
	t.Helper()
	for provider, ids := range asked {
		if want[provider] == "" {
			t.Errorf("%s asked for %v, want it not asked", provider, ids)
		}
	}
	for provider, id := range want {
		if !slices.Equal(asked[provider], []string{id}) {
			t.Errorf("%s asked for %v, want once for %q", provider, asked[provider], id)
		}
	}
}

func TestGetTopTracks_BridgedArtistFansOutToEveryBridgedIdPlusTheSeed(t *testing.T) {
	store := &search.FakeIdentityStore{
		Mbid: "mbid-che",
		Xref: map[search.ProviderKey]string{
			"spotify":    "spot-che",
			"applemusic": "apple-che",
		},
	}

	assertAsked(t, topTrackLookups(t, store, nil), map[search.ProviderName]string{
		search.ProviderDeezer:     "deezer-che",
		search.ProviderSpotify:    "spot-che",
		search.ProviderAppleMusic: "apple-che",
	})
}

func TestGetTopTracks_StoreMissAsksOnlyTheSeedProvider(t *testing.T) {
	assertAsked(t, topTrackLookups(t, &search.FakeIdentityStore{}, nil), map[search.ProviderName]string{
		search.ProviderDeezer: "deezer-che",
	})
}

func TestGetTopTracks_AppleMusicSharesTheITunesId(t *testing.T) {
	store := &search.FakeIdentityStore{
		Mbid: "mbid-x",
		Xref: map[search.ProviderKey]string{"itunes": "it1"},
	}

	assertAsked(t, topTrackLookups(t, store, nil), map[search.ProviderName]string{
		search.ProviderDeezer:     "deezer-che",
		search.ProviderAppleMusic: "it1",
	})
}

func TestGetTopTracks_NoIdentityStoreAsksTheSeedAndTheCallersKnownIds(t *testing.T) {
	known := map[search.ProviderName]string{search.ProviderDeezer: "deezer-che", search.ProviderSpotify: "spot-known"}

	assertAsked(t, topTrackLookups(t, nil, known), map[search.ProviderName]string{
		search.ProviderDeezer:  "deezer-che",
		search.ProviderSpotify: "spot-known",
	})
}
