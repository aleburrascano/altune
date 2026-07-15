package domain

import (
	"strconv"
	"strings"
)

// FeaturedArtist is a guest ("feat.") credit on a track: an immutable value
// object carrying a display name and, when the source provides it, a canonical
// id (MusicBrainz MBID and/or Deezer artist id). It is not an entity — it has no
// lifecycle beyond its membership on a track. Sourced by the FeaturedArtist
// resolver (MusicBrainz artist-credit + Deezer contributors) and carried on the
// wire in SearchResult.Extras["featured_artists"]. Introduced by
// docs/specs/featured-artists/spec.md; see ADR-0019.
type FeaturedArtist struct {
	Name     string
	MBID     string // MusicBrainz id, "" when unknown
	DeezerID int64  // Deezer artist id, 0 when unknown
	Role     string // "featured" (only value populated in v1)
}

// RoleFeatured is the only role populated in v1. The field is reserved so a
// later spec can carry producer/writer credits without a shape change.
const RoleFeatured = "featured"

// IdentityKey is the stable grouping key for "everything featuring X": MBID when
// present, else the Deezer id, else the normalized name. Two credits that share
// an identity key are the same artist.
func (f FeaturedArtist) IdentityKey() string {
	if f.MBID != "" {
		return "mb:" + f.MBID
	}
	if f.DeezerID != 0 {
		return "dz:" + strconv.FormatInt(f.DeezerID, 10)
	}
	return "name:" + NormalizeFeaturedName(f.Name)
}

// ToExtrasMap serializes the credit into the untyped wire map the client reads.
// Empty ids are omitted so absence stays distinguishable from a zero id.
func (f FeaturedArtist) ToExtrasMap() map[string]any {
	m := map[string]any{"name": f.Name, "role": f.Role}
	if f.MBID != "" {
		m["mbid"] = f.MBID
	}
	if f.DeezerID != 0 {
		m["deezer_id"] = f.DeezerID
	}
	return m
}

// FeaturedArtistsToExtras converts a slice to the []map[string]any extras shape.
// Returns nil for an empty slice so the key is omitted rather than emitted empty.
func FeaturedArtistsToExtras(fs []FeaturedArtist) []map[string]any {
	if len(fs) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.ToExtrasMap())
	}
	return out
}

// FeaturedArtistFromMap parses one wire map back into a value object. Tolerant of
// the numeric variance JSON round-trips introduce (int64 vs float64).
func FeaturedArtistFromMap(m map[string]any) FeaturedArtist {
	f := FeaturedArtist{
		Name: asString(m["name"]),
		MBID: asString(m["mbid"]),
		Role: asString(m["role"]),
	}
	if f.Role == "" {
		f.Role = RoleFeatured
	}
	switch v := m["deezer_id"].(type) {
	case int64:
		f.DeezerID = v
	case int:
		f.DeezerID = int64(v)
	case float64:
		f.DeezerID = int64(v)
	}
	return f
}

// NormalizeFeaturedName lower-cases and collapses whitespace so cross-provider
// credits for the same artist compare equal.
func NormalizeFeaturedName(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}
