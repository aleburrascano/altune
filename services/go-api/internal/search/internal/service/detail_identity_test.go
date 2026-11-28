package service_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/search/searchtest"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
)

type artistContentFuncs struct {
	topTracks func(ctx context.Context, provider domain.ProviderName, id string) ([]domain.SearchResult, error)
	albums    func(ctx context.Context, provider domain.ProviderName, id string) ([]domain.SearchResult, error)
}

func (p artistContentFuncs) GetArtistTopTracks(ctx context.Context, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
	if p.topTracks == nil {
		return nil, nil
	}
	return p.topTracks(ctx, provider, id)
}

func (p artistContentFuncs) GetArtistAlbums(ctx context.Context, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
	if p.albums == nil {
		return nil, nil
	}
	return p.albums(ctx, provider, id)
}

func topTrackLookups(t *testing.T, store ports.IdentityStore, known map[domain.ProviderName]string) map[domain.ProviderName][]string {
	t.Helper()
	var mu sync.Mutex
	asked := map[domain.ProviderName][]string{}
	providers := map[domain.ProviderName]ports.ArtistContentProvider{}
	for _, name := range []domain.ProviderName{domain.ProviderDeezer, domain.ProviderSpotify, domain.ProviderAppleMusic} {
		providers[name] = artistContentFuncs{topTracks: func(_ context.Context, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
			mu.Lock()
			defer mu.Unlock()
			asked[provider] = append(asked[provider], id)
			return nil, nil
		}}
	}
	var opts []searchtest.ArtistContentPart
	if store != nil {
		opts = append(opts, searchtest.ArtistIdentities(store))
	}
	svc := searchtest.NewArtistContent(t, providers, opts...)
	if _, err := svc.GetTopTracks(t.Context(), domain.ProviderDeezer, "deezer-che", known, 10); err != nil {
		t.Fatalf("GetTopTracks: %v", err)
	}
	return asked
}

func assertAsked(t *testing.T, asked map[domain.ProviderName][]string, want map[domain.ProviderName]string) {
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
	store := &memory.FakeIdentityStore{
		Mbid: "mbid-che",
		Xref: map[domain.ProviderKey]string{
			"spotify":    "spot-che",
			"applemusic": "apple-che",
		},
	}

	assertAsked(t, topTrackLookups(t, store, nil), map[domain.ProviderName]string{
		domain.ProviderDeezer:     "deezer-che",
		domain.ProviderSpotify:    "spot-che",
		domain.ProviderAppleMusic: "apple-che",
	})
}

func TestGetTopTracks_StoreMissAsksOnlyTheSeedProvider(t *testing.T) {
	assertAsked(t, topTrackLookups(t, &memory.FakeIdentityStore{}, nil), map[domain.ProviderName]string{
		domain.ProviderDeezer: "deezer-che",
	})
}

func TestGetTopTracks_AppleMusicSharesTheITunesId(t *testing.T) {
	store := &memory.FakeIdentityStore{
		Mbid: "mbid-x",
		Xref: map[domain.ProviderKey]string{"itunes": "it1"},
	}

	assertAsked(t, topTrackLookups(t, store, nil), map[domain.ProviderName]string{
		domain.ProviderDeezer:     "deezer-che",
		domain.ProviderAppleMusic: "it1",
	})
}

func TestGetTopTracks_NoIdentityStoreAsksTheSeedAndTheCallersKnownIds(t *testing.T) {
	known := map[domain.ProviderName]string{domain.ProviderDeezer: "deezer-che", domain.ProviderSpotify: "spot-known"}

	assertAsked(t, topTrackLookups(t, nil, known), map[domain.ProviderName]string{
		domain.ProviderDeezer:  "deezer-che",
		domain.ProviderSpotify: "spot-known",
	})
}

func TestSeedPathIdentity_ServesTheIdentityPathOnlyWithKnownIDs(t *testing.T) {
	ctx := context.Background()

	identity, serves := service.SeedPathIdentity{}.Identify(ctx, domain.ProviderDeezer, "dz-che", nil)
	if serves {
		t.Error("serves = true without known IDs, want the seed path")
	}
	if got := identity.ProviderIDs; len(got) != 1 || got[domain.ProviderDeezer] != "dz-che" || identity.MBID != "" {
		t.Errorf("identity = %+v, want just the seed id and no MBID", identity)
	}

	known := map[domain.ProviderName]string{domain.ProviderSpotify: "sp-che", domain.ProviderDeezer: "other"}
	identity, serves = service.SeedPathIdentity{}.Identify(ctx, domain.ProviderDeezer, "dz-che", known)
	if !serves {
		t.Error("serves = false with known IDs, want the identity path")
	}
	if got := identity.ProviderIDs; len(got) != 2 || got[domain.ProviderDeezer] != "dz-che" || got[domain.ProviderSpotify] != "sp-che" {
		t.Errorf("identity = %+v, want the seed id kept and the known spotify id added", identity)
	}
}

func TestStoreIdentity_AlwaysServesTheIdentityPath(t *testing.T) {
	ctx := context.Background()
	hit, err := service.NewStoreIdentity(&memory.FakeIdentityStore{Mbid: "mbid-che", Xref: map[domain.ProviderKey]string{"spotify": "sp-che"}})
	if err != nil {
		t.Fatalf("NewStoreIdentity: %v", err)
	}
	miss, err := service.NewStoreIdentity(&memory.FakeIdentityStore{})
	if err != nil {
		t.Fatalf("NewStoreIdentity: %v", err)
	}

	identity, serves := hit.Identify(ctx, domain.ProviderDeezer, "dz-che", nil)
	if !serves || identity.MBID != "mbid-che" || identity.ProviderIDs[domain.ProviderSpotify] != "sp-che" || identity.ProviderIDs[domain.ProviderDeezer] != "dz-che" {
		t.Errorf("hit: identity = %+v, serves = %v, want the store's MBID and ids plus the seed", identity, serves)
	}

	identity, serves = miss.Identify(ctx, domain.ProviderDeezer, "dz-che", nil)
	if !serves || identity.MBID != "" || len(identity.ProviderIDs) != 1 {
		t.Errorf("miss: identity = %+v, serves = %v, want the identity path with just the seed", identity, serves)
	}

	identity, _ = miss.Identify(ctx, domain.ProviderDeezer, "dz-che", map[domain.ProviderName]string{domain.ProviderSpotify: "sp-known"})
	if identity.ProviderIDs[domain.ProviderSpotify] != "sp-known" {
		t.Errorf("miss with known: identity = %+v, want the caller's known spotify id merged in", identity)
	}
}

func TestNewStoreIdentity_RefusesAMissingStore(t *testing.T) {
	identity, err := service.NewStoreIdentity(nil)

	if !errors.Is(err, domain.ErrArtistPageCollaboratorRequired) || identity != nil {
		t.Fatalf("identity = %v, err = %v, want artistpage.ErrCollaboratorRequired", identity, err)
	}
	if !strings.Contains(err.Error(), "identity store") {
		t.Errorf("err = %q, want it to name the identity store", err)
	}
}
