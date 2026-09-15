package domain

import (
	"fmt"
	"strconv"
	"strings"
)

type FeaturedArtist struct {
	Name     string
	MBID     string
	DeezerID int64
	Role     string
}

const RoleFeatured = "featured"

// MaxFeaturedArtistMBIDLength caps a featured artist's MBID. Every MBID the
// app handles is a MusicBrainz artist UUID in canonical 36-character form.
const MaxFeaturedArtistMBIDLength = 36

// ValidateFeaturedArtist caps a featured artist's name at the same length as
// every other free-text track field, and its MBID at the length of a UUID.
func ValidateFeaturedArtist(f FeaturedArtist) error {
	if len(f.Name) > maxTrackTextLength {
		return trackTextTooLongError("featured_artists name")
	}
	if len(f.MBID) > MaxFeaturedArtistMBIDLength {
		return NewValidationError(fmt.Sprintf("track featured_artists mbid exceeds %d characters", MaxFeaturedArtistMBIDLength))
	}
	return nil
}

// ValidateFeaturedArtists applies ValidateFeaturedArtist to every entry.
func ValidateFeaturedArtists(feats []FeaturedArtist) error {
	for _, f := range feats {
		if err := ValidateFeaturedArtist(f); err != nil {
			return err
		}
	}
	return nil
}

func NewFeaturedArtist(name, mbid string, deezerID int64) (FeaturedArtist, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return FeaturedArtist{}, false
	}
	return FeaturedArtist{
		Name:     name,
		MBID:     strings.TrimSpace(mbid),
		DeezerID: deezerID,
		Role:     RoleFeatured,
	}, true
}

func NewFeaturedArtistIdentityOnly(name, mbid string, deezerID int64) FeaturedArtist {
	return FeaturedArtist{
		Name:     strings.TrimSpace(name),
		MBID:     strings.TrimSpace(mbid),
		DeezerID: deezerID,
		Role:     RoleFeatured,
	}
}

func FeaturedArtistForQuery(name, mbid string, deezerID int64) FeaturedArtist {
	if fa, ok := NewFeaturedArtist(name, mbid, deezerID); ok {
		return fa
	}
	return NewFeaturedArtistIdentityOnly(name, mbid, deezerID)
}

func (f FeaturedArtist) NormalizedName() string {
	return strings.ToLower(strings.Join(strings.Fields(f.Name), " "))
}

func (f FeaturedArtist) IdentityKey() string {
	if f.MBID != "" {
		return f.MBID
	}
	if f.DeezerID != 0 {
		return "dz:" + strconv.FormatInt(f.DeezerID, 10)
	}
	return "name:" + f.NormalizedName()
}
