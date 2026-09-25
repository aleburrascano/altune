package service

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"testing"
)

func TestResolveArtistIdentity_bridged_fansOutIdsPlusSeed(t *testing.T) {
	store := &fakeIdentityStore{
		mbid: "mbid-che",
		xref: map[string]string{
			"spotify":    "spot-che",
			"applemusic": "apple-che",
		},
	}

	id, ok := resolveArtistIdentity(t.Context(), store, domain.ProviderDeezer, "deezer-che")
	if !ok {
		t.Fatal("ok = false, want true when the store has a bridge")
	}
	if id.MBID != "mbid-che" {
		t.Errorf("MBID = %q, want mbid-che", id.MBID)
	}
	if id.ProviderIDs[domain.ProviderDeezer] != "deezer-che" {
		t.Errorf("seed deezer id = %q, want deezer-che", id.ProviderIDs[domain.ProviderDeezer])
	}
	if id.ProviderIDs[domain.ProviderSpotify] != "spot-che" {
		t.Errorf("spotify id = %q, want spot-che", id.ProviderIDs[domain.ProviderSpotify])
	}
	if id.ProviderIDs[domain.ProviderAppleMusic] != "apple-che" {
		t.Errorf("applemusic id = %q, want apple-che", id.ProviderIDs[domain.ProviderAppleMusic])
	}
}

func TestResolveArtistIdentity_miss_seedOnly(t *testing.T) {
	id, ok := resolveArtistIdentity(t.Context(), &fakeIdentityStore{}, domain.ProviderDeezer, "deezer-che")
	if ok {
		t.Fatal("ok = true, want false on a store miss")
	}
	if len(id.ProviderIDs) != 1 || id.ProviderIDs[domain.ProviderDeezer] != "deezer-che" {
		t.Errorf("ProviderIDs = %v, want only the seed", id.ProviderIDs)
	}
	if id.MBID != "" {
		t.Errorf("MBID = %q, want empty on a miss", id.MBID)
	}
}

func TestProviderContentID_aliases(t *testing.T) {
	id := ResolvedArtistIdentity{
		MBID: "mbid-x",
		ProviderIDs: map[domain.ProviderName]string{
			domain.ProviderDeezer: "d1",
			domain.ProviderITunes: "it1",
		},
	}
	if got := providerContentID(id, domain.ProviderDeezer); got != "d1" {
		t.Errorf("deezer id = %q, want d1", got)
	}
	if got := providerContentID(id, domain.ProviderAppleMusic); got != "it1" {
		t.Errorf("apple music id = %q, want the shared iTunes id it1", got)
	}
	if got := providerContentID(id, domain.ProviderLastFM); got != "mbid-x" {
		t.Errorf("lastfm id = %q, want mbid-x", got)
	}
	if got := providerContentID(id, domain.ProviderSpotify); got != "" {
		t.Errorf("spotify id = %q, want empty (not bridged)", got)
	}
}

func TestResolveArtistIdentity_nilStore_seedOnly(t *testing.T) {
	id, ok := resolveArtistIdentity(t.Context(), nil, domain.ProviderDeezer, "deezer-che")
	if ok {
		t.Fatal("ok = true, want false with a nil store")
	}
	if id.ProviderIDs[domain.ProviderDeezer] != "deezer-che" {
		t.Errorf("seed missing: %v", id.ProviderIDs)
	}
}

type plainArtistProvider struct{}

func (plainArtistProvider) GetArtistTopTracks(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
	return nil, nil
}

func (plainArtistProvider) GetArtistAlbums(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
	return nil, nil
}

func TestResolveArtistIDByName(t *testing.T) {
	resolver := &fakeArtistContentProvider{
		resolveIDFn: func(_ context.Context, name string) (string, bool) {
			if name == "Che" {
				return "sc-42", true
			}
			return "", false
		},
	}
	if got := resolveArtistIDByName(context.Background(), resolver, "Che"); got != "sc-42" {
		t.Errorf("resolver hit = %q, want sc-42", got)
	}
	if got := resolveArtistIDByName(context.Background(), resolver, "Unknown"); got != "" {
		t.Errorf("resolver miss = %q, want empty (sit out, don't guess)", got)
	}
	if got := resolveArtistIDByName(context.Background(), resolver, ""); got != "" {
		t.Errorf("empty name = %q, want empty", got)
	}
	if got := resolveArtistIDByName(context.Background(), plainArtistProvider{}, "Che"); got != "" {
		t.Errorf("non-resolver provider = %q, want empty", got)
	}
}
