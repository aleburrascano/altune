package domain_test

import (
	"altune/go-api/internal/search/internal/domain"
	"testing"

	"pgregory.net/rapid"
)

var persistedProviderKeys = []struct {
	key  domain.ProviderKey
	want string
}{
	{domain.ProviderKeyDeezer, "deezer"},
	{domain.ProviderKeyMusicBrainz, "musicbrainz"},
	{domain.ProviderKeySoundCloud, "soundcloud"},
	{domain.ProviderKeyITunes, "itunes"},
	{domain.ProviderKeyTheAudioDB, "theaudiodb"},
	{domain.ProviderKeyDiscogs, "discogs"},
	{domain.ProviderKeyYouTube, "youtube"},
	{domain.ProviderKeyAppleMusic, "applemusic"},
	{domain.ProviderKeySpotify, "spotify"},
	{domain.ProviderKeyWikidata, "wikidata"},
	{domain.ProviderKeyCoverArtArchive, "coverartarchive"},
	{domain.ProviderKeyFanart, "fanart"},
	{domain.ProviderKeyGenius, "genius"},
	{domain.ProviderKeyYTMusic, "ytmusic"},
}

func TestProviderKey_PersistedStrings(t *testing.T) {
	for _, tt := range persistedProviderKeys {
		if got := tt.key.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
		got, err := domain.ParseProviderKey(tt.want)
		if err != nil || got != tt.key {
			t.Errorf("ParseProviderKey(%q) = (%q, %v), want %q", tt.want, got, err, tt.key)
		}
	}
}

const providerNameSweepLimit = 256

func TestProviderKey_EveryLiveProviderIsPinned(t *testing.T) {
	pinned := map[domain.ProviderKey]bool{}
	for _, tt := range persistedProviderKeys {
		pinned[tt.key] = true
	}
	live := map[domain.ProviderName]bool{}
	for _, p := range liveProviders {
		live[p] = true
	}
	for p := range domain.ProviderName(providerNameSweepLimit) {
		if p.String() == "unknown" {
			continue
		}
		if !pinned[p.Key()] {
			t.Errorf("provider %d has key %q, which no test pins; add it to the persisted-string table", int(p), p.Key())
		}
		if !live[p] {
			t.Errorf("provider %d (%v) is missing from liveProviders", int(p), p)
		}
	}
}

func TestParseProviderKey_RejectsUnknown(t *testing.T) {
	for _, in := range []string{"", "napster", "Deezer", "caa"} {
		if got, err := domain.ParseProviderKey(in); err == nil {
			t.Errorf("ParseProviderKey(%q) = %q, want error", in, got)
		}
	}
}

var liveProviders = []domain.ProviderName{
	domain.ProviderDeezer, domain.ProviderMusicBrainz, domain.ProviderSoundCloud, domain.ProviderITunes, domain.ProviderTheAudioDB,
	domain.ProviderDiscogs, domain.ProviderYouTube, domain.ProviderAppleMusic, domain.ProviderSpotify,
}

func TestProviderName_RetiredSlotsKeepTheirNumbersAndDecodeAsUnknown(t *testing.T) {
	wantNumbers := map[domain.ProviderName]int{
		domain.ProviderUnknown: 0, domain.ProviderDeezer: 1, domain.ProviderMusicBrainz: 2, domain.ProviderSoundCloud: 3, domain.ProviderITunes: 5,
		domain.ProviderTheAudioDB: 6, domain.ProviderDiscogs: 7, domain.ProviderYouTube: 8, domain.ProviderAppleMusic: 10, domain.ProviderSpotify: 11,
	}
	if len(wantNumbers) != len(liveProviders)+1 {
		t.Fatalf("pinned %d providers, want every live provider and unknown (%d)", len(wantNumbers), len(liveProviders)+1)
	}
	for p, n := range wantNumbers {
		if int(p) != n {
			t.Errorf("%v = %d, want %d so cached results keep their provider", p, int(p), n)
		}
	}
	for _, retired := range []domain.ProviderName{4, 9} {
		if retired.String() != "unknown" {
			t.Errorf("retired provider %d = %q, want unknown", int(retired), retired.String())
		}
	}
}

func TestProviderName_KeyRoundTrips(t *testing.T) {
	for _, p := range liveProviders {
		k := p.Key()
		if k.String() != p.String() {
			t.Errorf("%v.Key() = %q, want %q", p, k, p.String())
		}
		if _, err := domain.ParseProviderKey(k.String()); err != nil {
			t.Errorf("%v.Key() = %q is not a known key", p, k)
		}
		got, ok := k.ProviderName()
		if !ok || got != p {
			t.Errorf("%q.ProviderName() = (%v, %v), want %v", k, got, ok, p)
		}
	}
	if domain.ProviderUnknown.Key() != "unknown" {
		t.Errorf("ProviderUnknown.Key() = %q, want unknown", domain.ProviderUnknown.Key())
	}
}

func TestProviderKey_ProviderName_NonProviderKeys(t *testing.T) {
	for _, k := range []domain.ProviderKey{domain.ProviderKeyWikidata, domain.ProviderKeyCoverArtArchive, domain.ProviderKeyFanart, domain.ProviderKeyGenius, domain.ProviderKeyYTMusic, ""} {
		if p, ok := k.ProviderName(); ok {
			t.Errorf("%q.ProviderName() = %v, want not ok", k, p)
		}
	}
}

func TestProviderName_EveryNameRoundTripsToKnownKey(t *testing.T) {
	for _, p := range liveProviders {
		got, err := domain.ParseProviderName(p.String())
		if err != nil || got != p {
			t.Errorf("ParseProviderName(%q) = (%v, %v), want %v", p.String(), got, err, p)
		}
		if _, err := domain.ParseProviderKey(p.Key().String()); err != nil {
			t.Errorf("%v.Key() = %q is not a known provider key: %v", p, p.Key(), err)
		}
	}
}

func TestParseProviderKey_AcceptedKeysRoundTripAndTheRestAreRefused(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		raw := rapid.String().Draw(t, "raw")

		key, err := domain.ParseProviderKey(raw)
		if err != nil {
			if key != "" {
				t.Fatalf("ParseProviderKey(%q) refused but returned key %q", raw, key)
			}
			return
		}
		if key.String() != raw {
			t.Fatalf("ParseProviderKey(%q) = %q, want the same text back", raw, key)
		}
		if again, err := domain.ParseProviderKey(key.String()); err != nil || again != key {
			t.Fatalf("an accepted key %q did not parse again: %v, %v", key, again, err)
		}
	})
}
