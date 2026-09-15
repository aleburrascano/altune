package domain

import "fmt"

type ResultKind int

const (
	ResultKindUnknown ResultKind = iota
	ResultKindArtist
	ResultKindAlbum
	ResultKindTrack
	ResultKindPlaylist
)

func (k ResultKind) String() string {
	switch k {
	case ResultKindUnknown:
		return "unknown"
	case ResultKindArtist:
		return "artist"
	case ResultKindAlbum:
		return "album"
	case ResultKindTrack:
		return "track"
	case ResultKindPlaylist:
		return "playlist"
	default:
		return "unknown"
	}
}

func ParseResultKind(s string) (ResultKind, error) {
	switch s {
	case "artist":
		return ResultKindArtist, nil
	case "album":
		return ResultKindAlbum, nil
	case "track":
		return ResultKindTrack, nil
	case "playlist":
		return ResultKindPlaylist, nil
	default:
		return 0, fmt.Errorf("unknown result kind: %s", s)
	}
}

type Confidence int

const (
	ConfidenceLow Confidence = iota
	ConfidenceMedium
	ConfidenceHigh
)

func (c Confidence) String() string {
	switch c {
	case ConfidenceHigh:
		return "high"
	case ConfidenceMedium:
		return "medium"
	case ConfidenceLow:
		return "low"
	default:
		return "unknown"
	}
}

type EntityResolutionTier int

const (
	EntityResolutionNone EntityResolutionTier = iota
	EntityResolutionISRC
	EntityResolutionUPC
	EntityResolutionMBID
	EntityResolutionBridge
)

func (t EntityResolutionTier) String() string {
	switch t {
	case EntityResolutionMBID:
		return "mbid"
	case EntityResolutionISRC:
		return "isrc"
	case EntityResolutionUPC:
		return "upc"
	case EntityResolutionBridge:
		return "bridge"
	case EntityResolutionNone:
		return "none"
	default:
		return "unknown"
	}
}

func ResolutionTierFromExtras(extras map[string]any) EntityResolutionTier {
	s, _ := extras["resolution_tier"].(string)
	switch s {
	case "mbid":
		return EntityResolutionMBID
	case "isrc":
		return EntityResolutionISRC
	case "upc":
		return EntityResolutionUPC
	case "bridge":
		return EntityResolutionBridge
	default:
		return EntityResolutionNone
	}
}

type ProviderName int

const (
	ProviderUnknown ProviderName = iota
	ProviderDeezer
	ProviderMusicBrainz
	ProviderSoundCloud
	ProviderLastFM
	ProviderITunes
	ProviderTheAudioDB
	ProviderDiscogs
	ProviderYouTube
	ProviderAmazonMusic
	ProviderAppleMusic
	ProviderSpotify
)

// CanonicalContentProvider is the provider whose IDs back browseable content:
// album-track and artist-album lookups, featured-artist enrichment, and the
// head of the artist-content fan-out all resolve through it. Changing the
// canonical provider means changing this one constant.
const CanonicalContentProvider = ProviderDeezer

// IsCanonicalContentProvider reports whether p is the canonical content provider.
func IsCanonicalContentProvider(p ProviderName) bool {
	return p == CanonicalContentProvider
}

func (p ProviderName) String() string {
	switch p {
	case ProviderUnknown:
		return "unknown"
	case ProviderDeezer:
		return "deezer"
	case ProviderMusicBrainz:
		return "musicbrainz"
	case ProviderSoundCloud:
		return "soundcloud"
	case ProviderLastFM:
		return "lastfm"
	case ProviderITunes:
		return "itunes"
	case ProviderTheAudioDB:
		return "theaudiodb"
	case ProviderDiscogs:
		return "discogs"
	case ProviderYouTube:
		return "youtube"
	case ProviderAmazonMusic:
		return "amazonmusic"
	case ProviderAppleMusic:
		return "applemusic"
	case ProviderSpotify:
		return "spotify"
	default:
		return "unknown"
	}
}

func ParseProviderName(s string) (ProviderName, error) {
	switch s {
	case "deezer":
		return ProviderDeezer, nil
	case "musicbrainz":
		return ProviderMusicBrainz, nil
	case "soundcloud":
		return ProviderSoundCloud, nil
	case "lastfm":
		return ProviderLastFM, nil
	case "itunes":
		return ProviderITunes, nil
	case "theaudiodb":
		return ProviderTheAudioDB, nil
	case "discogs":
		return ProviderDiscogs, nil
	case "youtube":
		return ProviderYouTube, nil
	case "amazonmusic":
		return ProviderAmazonMusic, nil
	case "applemusic":
		return ProviderAppleMusic, nil
	case "spotify":
		return ProviderSpotify, nil
	default:
		return 0, fmt.Errorf("unknown provider: %s", s)
	}
}

type ProviderStatus int

const (
	ProviderStatusOK ProviderStatus = iota
	ProviderStatusTimeout
	ProviderStatusError
	ProviderStatusRateLimited
	ProviderStatusCircuitOpen
)

func (s ProviderStatus) String() string {
	switch s {
	case ProviderStatusOK:
		return "ok"
	case ProviderStatusTimeout:
		return "timeout"
	case ProviderStatusError:
		return "error"
	case ProviderStatusRateLimited:
		return "rate_limited"
	case ProviderStatusCircuitOpen:
		return "circuit_open"
	default:
		return "unknown"
	}
}
