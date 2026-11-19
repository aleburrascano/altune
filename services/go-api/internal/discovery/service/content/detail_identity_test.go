package content_test

import (
	"altune/go-api/internal/discovery"
	"context"
	"slices"
	"sync"
	"testing"
)

type artistContentFuncs struct {
	topTracks func(ctx context.Context, provider discovery.ProviderName, id string) ([]discovery.SearchResult, error)
	albums    func(ctx context.Context, provider discovery.ProviderName, id string) ([]discovery.SearchResult, error)
}

func (p artistContentFuncs) GetArtistTopTracks(ctx context.Context, provider discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
	if p.topTracks == nil {
		return nil, nil
	}
	return p.topTracks(ctx, provider, id)
}

func (p artistContentFuncs) GetArtistAlbums(ctx context.Context, provider discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
	if p.albums == nil {
		return nil, nil
	}
	return p.albums(ctx, provider, id)
}

func topTrackLookups(t *testing.T, store discovery.IdentityStore, known map[discovery.ProviderName]string) map[discovery.ProviderName][]string {
	t.Helper()
	var mu sync.Mutex
	asked := map[discovery.ProviderName][]string{}
	providers := map[discovery.ProviderName]discovery.ArtistContentProvider{}
	for _, name := range []discovery.ProviderName{discovery.ProviderDeezer, discovery.ProviderSpotify, discovery.ProviderAppleMusic} {
		providers[name] = artistContentFuncs{topTracks: func(_ context.Context, provider discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
			mu.Lock()
			defer mu.Unlock()
			asked[provider] = append(asked[provider], id)
			return nil, nil
		}}
	}
	var opts []discovery.ArtistContentOption
	if store != nil {
		opts = append(opts, discovery.WithContentIdentityStore(store))
	}
	svc := discovery.NewGetArtistContentService(providers, opts...)
	if _, err := svc.GetTopTracks(t.Context(), discovery.ProviderDeezer, "deezer-che", known, 10); err != nil {
		t.Fatalf("GetTopTracks: %v", err)
	}
	return asked
}

func assertAsked(t *testing.T, asked map[discovery.ProviderName][]string, want map[discovery.ProviderName]string) {
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
	store := &discovery.FakeIdentityStore{
		Mbid: "mbid-che",
		Xref: map[discovery.ProviderKey]string{
			"spotify":    "spot-che",
			"applemusic": "apple-che",
		},
	}

	assertAsked(t, topTrackLookups(t, store, nil), map[discovery.ProviderName]string{
		discovery.ProviderDeezer:     "deezer-che",
		discovery.ProviderSpotify:    "spot-che",
		discovery.ProviderAppleMusic: "apple-che",
	})
}

func TestGetTopTracks_StoreMissAsksOnlyTheSeedProvider(t *testing.T) {
	assertAsked(t, topTrackLookups(t, &discovery.FakeIdentityStore{}, nil), map[discovery.ProviderName]string{
		discovery.ProviderDeezer: "deezer-che",
	})
}

func TestGetTopTracks_AppleMusicSharesTheITunesId(t *testing.T) {
	store := &discovery.FakeIdentityStore{
		Mbid: "mbid-x",
		Xref: map[discovery.ProviderKey]string{"itunes": "it1"},
	}

	assertAsked(t, topTrackLookups(t, store, nil), map[discovery.ProviderName]string{
		discovery.ProviderDeezer:     "deezer-che",
		discovery.ProviderAppleMusic: "it1",
	})
}

func TestGetTopTracks_NoIdentityStoreAsksTheSeedAndTheCallersKnownIds(t *testing.T) {
	known := map[discovery.ProviderName]string{discovery.ProviderDeezer: "deezer-che", discovery.ProviderSpotify: "spot-known"}

	assertAsked(t, topTrackLookups(t, nil, known), map[discovery.ProviderName]string{
		discovery.ProviderDeezer:  "deezer-che",
		discovery.ProviderSpotify: "spot-known",
	})
}
