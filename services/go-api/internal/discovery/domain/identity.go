package domain

import (
	"fmt"
	"strings"
)

// AlbumVerdict is the identity resolution outcome for an album.
type AlbumVerdict int

const (
	AlbumVerdictUnknown       AlbumVerdict = iota // 0 = default/unset
	AlbumVerdictConfirmed                         // definitely by this artist
	AlbumVerdictContamination                     // definitely NOT by this artist
	AlbumVerdictSuspect                           // likely not, pending 24h safeguard
)

func (v AlbumVerdict) String() string {
	switch v {
	case AlbumVerdictUnknown:
		return "unknown"
	case AlbumVerdictConfirmed:
		return "confirmed"
	case AlbumVerdictContamination:
		return "contamination"
	case AlbumVerdictSuspect:
		return "suspect"
	default:
		return "unknown"
	}
}

func ParseAlbumVerdict(s string) (AlbumVerdict, error) {
	switch s {
	case "unknown":
		return AlbumVerdictUnknown, nil
	case "confirmed":
		return AlbumVerdictConfirmed, nil
	case "contamination":
		return AlbumVerdictContamination, nil
	case "suspect":
		return AlbumVerdictSuspect, nil
	default:
		return 0, fmt.Errorf("unknown album verdict: %s", s)
	}
}

// ArtistIdentityProfile carries identity signals accumulated from multiple
// providers. It is a read-model assembled at query time, not an aggregate.
type ArtistIdentityProfile struct {
	MBID                 string
	DiscogsID            int
	BirthYear            int
	Area                 string          // city/country from MB or Discogs
	ArtistType           string          // "Person" or "Group" from MB
	GenreCluster         map[string]bool // set of genre strings from all providers
	KnownISRCRegistrants map[string]bool // set of ISRC registrant codes from confirmed albums
	Disambiguation       string          // from MB
	MBConfirmedTitles    map[string]bool // normalized titles confirmed by MB release-groups
}

// NewArtistIdentityProfile returns a profile with initialized maps.
func NewArtistIdentityProfile() ArtistIdentityProfile {
	return ArtistIdentityProfile{
		GenreCluster:         map[string]bool{},
		KnownISRCRegistrants: map[string]bool{},
		MBConfirmedTitles:    map[string]bool{},
	}
}

// AddGenre adds a normalized (lowercased, hyphens→spaces) genre to the cluster.
func (p *ArtistIdentityProfile) AddGenre(genre string) {
	p.GenreCluster[normalizeGenre(genre)] = true
}

// AddISRCRegistrant adds a registrant code to the known set.
func (p *ArtistIdentityProfile) AddISRCRegistrant(registrant string) {
	p.KnownISRCRegistrants[registrant] = true
}

// HasGenreOverlap returns true if any genre in the list exists in GenreCluster.
func (p *ArtistIdentityProfile) HasGenreOverlap(genres []string) bool {
	for _, g := range genres {
		if p.GenreCluster[normalizeGenre(g)] {
			return true
		}
	}
	return false
}

func normalizeGenre(g string) string {
	return strings.ToLower(strings.ReplaceAll(g, "-", " "))
}

// ExtractISRCRegistrant extracts the registrant code (characters 2-6) from an
// ISRC string. Returns empty string if the ISRC is too short or malformed.
// ISRC format: CC-XXX-YY-NNNNN or CCXXXYYNNNNN (12 chars without dashes).
func ExtractISRCRegistrant(isrc string) string {
	// Strip dashes to normalize format
	normalized := strings.ReplaceAll(isrc, "-", "")
	if len(normalized) < 6 {
		return ""
	}
	return normalized[2:6]
}
