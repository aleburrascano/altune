package domain

import (
	"altune/go-api/internal/shared/textnorm"
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

// ValidateFeaturedArtist holds a featured artist's name to the same length as
// every other free-text track field, and its MBID to the length of a UUID.
func ValidateFeaturedArtist(f FeaturedArtist) error {
	if err := validateFeaturedArtistName(f.Name); err != nil {
		return err
	}
	return validateFeaturedArtistMBID(f.MBID)
}

func validateFeaturedArtistName(name string) error {
	if err := ValidateText(name, "track featured_artists name"); err != nil {
		return err
	}
	if len(name) > maxTrackTextLength {
		return trackTextTooLongError("featured_artists name")
	}
	return nil
}

func validateFeaturedArtistMBID(mbid string) error {
	if err := ValidateText(mbid, "track featured_artists mbid"); err != nil {
		return err
	}
	if len(mbid) > MaxFeaturedArtistMBIDLength {
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

// NormalizedName is the name fold behind the name-based identity key (the
// featured_artists.norm_name column). It applies NFKC like the track dedup
// normalization so Unicode-equivalent spellings coalesce into one row.
func (f FeaturedArtist) NormalizedName() string {
	return textnorm.FoldName(f.Name)
}

// LegacyIdentityKey is the identity key as computed before NormalizedName
// applied NFKC. Rows persisted then carry it, so readers and the upsert match
// on it as well as IdentityKey. It equals IdentityKey for MBID/Deezer keys and
// for names that NFKC leaves unchanged.
func (f FeaturedArtist) LegacyIdentityKey() string {
	if f.MBID != "" || f.DeezerID != 0 {
		return f.IdentityKey()
	}
	return "name:" + strings.ToLower(strings.Join(strings.Fields(f.Name), " "))
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
