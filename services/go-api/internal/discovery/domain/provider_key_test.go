package domain

import "testing"

// The key strings are persisted (Postgres entity_identity.provider, Redis
// identity and artwork caches), so each constant is pinned byte-for-byte.
func TestProviderKey_PersistedStrings(t *testing.T) {
	tests := []struct {
		key  ProviderKey
		want string
	}{
		{ProviderKeyDeezer, "deezer"},
		{ProviderKeyMusicBrainz, "musicbrainz"},
		{ProviderKeySoundCloud, "soundcloud"},
		{ProviderKeyLastFM, "lastfm"},
		{ProviderKeyITunes, "itunes"},
		{ProviderKeyTheAudioDB, "theaudiodb"},
		{ProviderKeyDiscogs, "discogs"},
		{ProviderKeyYouTube, "youtube"},
		{ProviderKeyAmazonMusic, "amazonmusic"},
		{ProviderKeyAppleMusic, "applemusic"},
		{ProviderKeySpotify, "spotify"},
		{ProviderKeyWikidata, "wikidata"},
		{ProviderKeyCoverArtArchive, "coverartarchive"},
		{ProviderKeyFanart, "fanart"},
		{ProviderKeyGenius, "genius"},
		{ProviderKeyYTMusic, "ytmusic"},
	}
	if len(tests) != len(knownProviderKeys) {
		t.Fatalf("pinned %d keys, vocabulary has %d", len(tests), len(knownProviderKeys))
	}
	for _, tt := range tests {
		if got := tt.key.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
		got, err := ParseProviderKey(tt.want)
		if err != nil || got != tt.key {
			t.Errorf("ParseProviderKey(%q) = (%q, %v), want %q", tt.want, got, err, tt.key)
		}
	}
}

func TestParseProviderKey_RejectsUnknown(t *testing.T) {
	for _, in := range []string{"", "napster", "Deezer", "caa"} {
		if got, err := ParseProviderKey(in); err == nil {
			t.Errorf("ParseProviderKey(%q) = %q, want error", in, got)
		}
	}
}

func TestProviderName_KeyRoundTrips(t *testing.T) {
	for p := ProviderDeezer; p <= ProviderSpotify; p++ {
		k := p.Key()
		if k.String() != p.String() {
			t.Errorf("%v.Key() = %q, want %q", p, k, p.String())
		}
		if _, err := ParseProviderKey(k.String()); err != nil {
			t.Errorf("%v.Key() = %q is not a known key", p, k)
		}
		got, ok := k.ProviderName()
		if !ok || got != p {
			t.Errorf("%q.ProviderName() = (%v, %v), want %v", k, got, ok, p)
		}
	}
	if ProviderUnknown.Key() != "unknown" {
		t.Errorf("ProviderUnknown.Key() = %q, want unknown", ProviderUnknown.Key())
	}
}

func TestProviderKey_ProviderName_NonProviderKeys(t *testing.T) {
	for _, k := range []ProviderKey{ProviderKeyWikidata, ProviderKeyCoverArtArchive, ProviderKeyFanart, ProviderKeyGenius, ProviderKeyYTMusic, ""} {
		if p, ok := k.ProviderName(); ok {
			t.Errorf("%q.ProviderName() = %v, want not ok", k, p)
		}
	}
}
