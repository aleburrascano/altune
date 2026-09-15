package domain

import "fmt"

// ProviderKey is the string identity of a provider wherever that identity is
// keyed by name rather than by ProviderName: the keys of an xref / external-ID
// map, the provider column of a durable identity row, and the source tag of a
// resolved artwork URL. Its value is persisted verbatim (Postgres identity
// rows, Redis identity and artwork caches), so a constant's string must never
// change.
//
// The vocabulary is wider than ProviderName: it also names external-ID
// namespaces that are not search providers (wikidata) and artwork-only
// sources (coverartarchive, fanart, genius, ytmusic). Note the xref key for an
// Apple Music link is "itunes", not "applemusic".
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

// ParseProviderKey validates s against the known key vocabulary.
func ParseProviderKey(s string) (ProviderKey, error) {
	k := ProviderKey(s)
	if _, ok := knownProviderKeys[k]; !ok {
		return "", fmt.Errorf("unknown provider key: %s", s)
	}
	return k, nil
}

func (k ProviderKey) String() string { return string(k) }

// ProviderName maps k to the search provider of the same name; ok is false for
// keys that name no search provider (e.g. wikidata, fanart).
func (k ProviderKey) ProviderName() (ProviderName, bool) {
	p, err := ParseProviderName(string(k))
	if err != nil {
		return ProviderUnknown, false
	}
	return p, true
}

// Key returns the ProviderKey of the same name as p.
func (p ProviderName) Key() ProviderKey { return ProviderKey(p.String()) }
