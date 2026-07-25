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

func (f FeaturedArtist) IdentityKey() string {
	if f.MBID != "" {
		return "mb:" + f.MBID
	}
	if f.DeezerID != 0 {
		return "dz:" + strconv.FormatInt(f.DeezerID, 10)
	}
	return "name:" + NormalizeFeaturedName(f.Name)
}

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

func FeaturedArtistsFromExtras(extras map[string]any) []FeaturedArtist {
	raw, ok := extras["featured_artists"].([]map[string]any)
	if !ok {
		return nil
	}
	out := make([]FeaturedArtist, 0, len(raw))
	for _, m := range raw {
		out = append(out, FeaturedArtistFromMap(m))
	}
	return out
}

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

func NormalizeFeaturedName(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}
