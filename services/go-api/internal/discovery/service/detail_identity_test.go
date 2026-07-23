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

func TestResolveArtistIdentity_nilStore_seedOnly(t *testing.T) {
	id, ok := resolveArtistIdentity(t.Context(), nil, domain.ProviderDeezer, "deezer-che")
	if ok {
		t.Fatal("ok = true, want false with a nil store")
	}
	if id.ProviderIDs[domain.ProviderDeezer] != "deezer-che" {
		t.Errorf("seed missing: %v", id.ProviderIDs)
	}
}
