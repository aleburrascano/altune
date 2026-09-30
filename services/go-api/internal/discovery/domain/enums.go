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

type ResolutionTierStamp struct {
	Tier    EntityResolutionTier
	Stamped bool
}

func StampResolutionTier(tier EntityResolutionTier) ResolutionTierStamp {
	return ResolutionTierStamp{Tier: tier, Stamped: true}
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

const CanonicalContentProvider = ProviderDeezer

func IsCanonicalContentProvider(p ProviderName) bool {
	return p == CanonicalContentProvider
}

func (p ProviderName) String() string {
	if p <= ProviderUnknown || int(p) >= len(providerKeys) {
		return string(providerKeys[ProviderUnknown])
	}
	return string(providerKeys[p])
}

func ParseProviderName(s string) (ProviderName, error) {
	if p, ok := providerNamesByKey[ProviderKey(s)]; ok {
		return p, nil
	}
	return 0, fmt.Errorf("unknown provider: %s", s)
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
