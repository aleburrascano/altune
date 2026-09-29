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

var knownProviderKeys = map[ProviderKey]struct{}{
	ProviderKeyDeezer:          {},
	ProviderKeyMusicBrainz:     {},
	ProviderKeySoundCloud:      {},
	ProviderKeyLastFM:          {},
	ProviderKeyITunes:          {},
	ProviderKeyTheAudioDB:      {},
	ProviderKeyDiscogs:         {},
	ProviderKeyYouTube:         {},
	ProviderKeyAmazonMusic:     {},
	ProviderKeyAppleMusic:      {},
	ProviderKeySpotify:         {},
	ProviderKeyWikidata:        {},
	ProviderKeyCoverArtArchive: {},
	ProviderKeyFanart:          {},
	ProviderKeyGenius:          {},
	ProviderKeyYTMusic:         {},
}

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
