package domain

import (
	"fmt"
	"strings"
)

type AlbumVerdict int

const (
	AlbumVerdictUnknown AlbumVerdict = iota
	AlbumVerdictConfirmed
	AlbumVerdictContamination
	AlbumVerdictSuspect
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

type ArtistIdentityProfile struct {
	MBID                 string
	DiscogsID            int
	BirthYear            int
	Area                 string
	ArtistType           string
	GenreCluster         map[string]bool
	KnownISRCRegistrants map[string]bool
	Disambiguation       string
	MBConfirmedTitles    map[string]bool
}

func NewArtistIdentityProfile() ArtistIdentityProfile {
	return ArtistIdentityProfile{
		GenreCluster:         map[string]bool{},
		KnownISRCRegistrants: map[string]bool{},
		MBConfirmedTitles:    map[string]bool{},
	}
}

func (p *ArtistIdentityProfile) AddGenre(genre string) {
	p.GenreCluster[normalizeGenre(genre)] = true
}

func (p *ArtistIdentityProfile) AddISRCRegistrant(registrant string) {
	p.KnownISRCRegistrants[registrant] = true
}

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

func ExtractISRCRegistrant(isrc string) string {
	normalized := strings.ReplaceAll(isrc, "-", "")
	if len(normalized) < 6 {
		return ""
	}
	return normalized[2:6]
}
