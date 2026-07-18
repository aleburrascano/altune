package domain

import (
	"strconv"
	"strings"
)

// FeaturedArtist is a guest ("feat.") credit persisted on a Track in the catalog
// context: an immutable value object carrying a display name and, when known, a
// canonical id (MusicBrainz MBID and/or Deezer artist id). It is not an entity —
// it has no lifecycle beyond its membership on a track. Sourced from the
// discovery FeaturedArtistResolver (via the catalog bridge). See ADR-0019.
type FeaturedArtist struct {
	Name     string
	MBID     string // "" when unknown
	DeezerID int64  // 0 when unknown
	Role     string // "featured" (only value populated in v1)
}

// RoleFeatured is the only role populated in v1.
const RoleFeatured = "featured"

// NewFeaturedArtist trims the name and defaults the role. An empty name yields
// (zero, false) so callers can drop unnamed credits.
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

// NewFeaturedArtistIdentityOnly builds a FeaturedArtist for identity-based
// lookups (e.g. "list tracks featuring this artist") where the caller has an
// mbid or deezer id but no display name. Unlike NewFeaturedArtist, it does not
// require a non-empty name — IdentityKey still resolves from mbid/deezerID.
func NewFeaturedArtistIdentityOnly(name, mbid string, deezerID int64) FeaturedArtist {
	return FeaturedArtist{
		Name:     strings.TrimSpace(name),
		MBID:     strings.TrimSpace(mbid),
		DeezerID: deezerID,
		Role:     RoleFeatured,
	}
}

// FeaturedArtistForQuery builds a FeaturedArtist for query operations (e.g.
// "list tracks featuring this artist") from raw HTTP parameters. Uses the full
// constructor when a non-empty name is present; falls back to identity-only so
// mbid/deezerID still resolve an IdentityKey without a display name.
func FeaturedArtistForQuery(name, mbid string, deezerID int64) FeaturedArtist {
	if fa, ok := NewFeaturedArtist(name, mbid, deezerID); ok {
		return fa
	}
	return NewFeaturedArtistIdentityOnly(name, mbid, deezerID)
}

// NormalizedName lower-cases and collapses whitespace — the name component of the
// identity key and the fallback grouping key.
func (f FeaturedArtist) NormalizedName() string {
	return strings.ToLower(strings.Join(strings.Fields(f.Name), " "))
}

// IdentityKey is the stable grouping key for "everything featuring X": MBID, else
// the Deezer id, else the normalized name. Mirrors the generated column on the
// featured_artists table so Go-side and SQL-side identity agree.
func (f FeaturedArtist) IdentityKey() string {
	if f.MBID != "" {
		return f.MBID
	}
	if f.DeezerID != 0 {
		return "dz:" + strconv.FormatInt(f.DeezerID, 10)
	}
	return "name:" + f.NormalizedName()
}
