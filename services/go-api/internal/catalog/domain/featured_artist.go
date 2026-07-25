package domain

import (
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
