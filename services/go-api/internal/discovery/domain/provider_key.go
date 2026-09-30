package domain

import "fmt"

type ProviderKey string

const (
	ProviderKeyDeezer          ProviderKey = "deezer"
	ProviderKeyMusicBrainz     ProviderKey = "musicbrainz"
	ProviderKeySoundCloud      ProviderKey = "soundcloud"
	ProviderKeyLastFM          ProviderKey = "lastfm"
	ProviderKeyITunes          ProviderKey = "itunes"
	ProviderKeyTheAudioDB      ProviderKey = "theaudiodb"
	ProviderKeyDiscogs         ProviderKey = "discogs"
	ProviderKeyYouTube         ProviderKey = "youtube"
	ProviderKeyAmazonMusic     ProviderKey = "amazonmusic"
	ProviderKeyAppleMusic      ProviderKey = "applemusic"
	ProviderKeySpotify         ProviderKey = "spotify"
	ProviderKeyWikidata        ProviderKey = "wikidata"
	ProviderKeyCoverArtArchive ProviderKey = "coverartarchive"
	ProviderKeyFanart          ProviderKey = "fanart"
	ProviderKeyGenius          ProviderKey = "genius"
	ProviderKeyYTMusic         ProviderKey = "ytmusic"
)

var providerKeys = [...]ProviderKey{
	ProviderUnknown:     "unknown",
	ProviderDeezer:      ProviderKeyDeezer,
	ProviderMusicBrainz: ProviderKeyMusicBrainz,
	ProviderSoundCloud:  ProviderKeySoundCloud,
	ProviderLastFM:      ProviderKeyLastFM,
	ProviderITunes:      ProviderKeyITunes,
	ProviderTheAudioDB:  ProviderKeyTheAudioDB,
	ProviderDiscogs:     ProviderKeyDiscogs,
	ProviderYouTube:     ProviderKeyYouTube,
	ProviderAmazonMusic: ProviderKeyAmazonMusic,
	ProviderAppleMusic:  ProviderKeyAppleMusic,
	ProviderSpotify:     ProviderKeySpotify,
}

var providerNamesByKey = func() map[ProviderKey]ProviderName {
	m := make(map[ProviderKey]ProviderName, len(providerKeys))
	for p := ProviderUnknown + 1; int(p) < len(providerKeys); p++ {
		m[providerKeys[p]] = p
	}
	return m
}()

var knownProviderKeys = func() map[ProviderKey]struct{} {
	m := map[ProviderKey]struct{}{
		ProviderKeyWikidata:        {},
		ProviderKeyCoverArtArchive: {},
		ProviderKeyFanart:          {},
		ProviderKeyGenius:          {},
		ProviderKeyYTMusic:         {},
	}
	for k := range providerNamesByKey {
		m[k] = struct{}{}
	}
	return m
}()

func ParseProviderKey(s string) (ProviderKey, error) {
	k := ProviderKey(s)
	if _, ok := knownProviderKeys[k]; !ok {
		return "", fmt.Errorf("unknown provider key: %s", s)
	}
	return k, nil
}

func (k ProviderKey) String() string { return string(k) }

func (k ProviderKey) ProviderName() (ProviderName, bool) {
	p, err := ParseProviderName(string(k))
	if err != nil {
		return ProviderUnknown, false
	}
	return p, true
}

func (p ProviderName) Key() ProviderKey { return ProviderKey(p.String()) }
