package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// fakeIdentityStore is defined in artwork_fill_test.go; it returns ok=true iff
// mbid is non-empty, so a zero-value store models a miss.

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
	// Seed always present, even though xref did not carry the Deezer id.
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
	// Apple Music shares the iTunes catalog id (the bridge only emits "itunes").
	if got := providerContentID(id, domain.ProviderAppleMusic); got != "it1" {
		t.Errorf("apple music id = %q, want the shared iTunes id it1", got)
	}
	// Last.fm keys on MBID.
	if got := providerContentID(id, domain.ProviderLastFM); got != "mbid-x" {
		t.Errorf("lastfm id = %q, want mbid-x", got)
	}
	// A provider with no resolved id and no alias sits out.
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
