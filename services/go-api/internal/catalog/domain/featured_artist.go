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

const MaxFeaturedArtistMBIDLength = 36

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
	return textnorm.FoldName(f.Name)
}

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
